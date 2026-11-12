package connector

import (
	"context"
	"sync/atomic"
)

// Operation lends sequential device methods to a single composite operation.
// It expires when its callback returns and never permits observer reentry.
// Do not copy Operation.
type Operation struct {
	device     *Device
	scope      *callScope
	borrowed   bool
	retired    atomic.Bool
	cleanupErr error // Owned by the admitted call; read after scope.end joins it.
}

// WithOperation admits one sequential composite operation. The callback context
// includes cancellation from Close; use it for nested I/O and application work.
// The borrowed Operation expires when run returns, even after an error.
func (d *Device) WithOperation(ctx context.Context, run func(context.Context, *Operation) error) (err error) {
	if run == nil {
		return ErrInvalid
	}
	if err = ctx.Err(); err != nil {
		return
	}
	if err = d.acquire(); err != nil {
		return
	}
	defer d.release()
	ctx, cancel := d.operationContext(ctx)
	defer cancel()
	returned := false
	o := &Operation{device: d, scope: newCallScope(ctx)}
	d.operation = o
	defer func() {
		defer func() { d.operation = nil }()
		if o.scope.end() && err == nil {
			err = context.Canceled
		}
		if !returned {
			_ = d.closeResources()
		} else if err != nil {
			err = joinCleanup(err, d.closeResources())
		}
	}()
	err = run(ctx, o)
	returned = true
	if err == nil {
		err = ctx.Err()
	}
	return
}

// WithBorrowedOperation lends an already connected session to run. It holds one
// lease for the entire callback and never connects, reconnects or replays a
// command. A failed callback retires the session. Cleanup failures are returned
// separately, including when run ignores a failed Retire or returns nil.
// Like WithOperation, it cancels and joins calls still active when run returns.
func (d *Device) WithBorrowedOperation(ctx context.Context, run func(context.Context, *Operation) error) (operationErr, cleanupErr error) {
	if run == nil {
		return ErrInvalid, nil
	}
	if err := ctx.Err(); err != nil {
		return err, nil
	}
	if err := d.acquire(); err != nil {
		return err, nil
	}
	defer d.release()
	ctx, cancel := d.operationContext(ctx)
	defer cancel()
	if !d.Connected() {
		return ErrNotConnected, nil
	}
	o := &Operation{device: d, scope: newCallScope(ctx), borrowed: true}
	d.operation = o
	returned := false
	defer func() {
		defer func() { d.operation = nil }()
		if o.scope.end() && operationErr == nil {
			operationErr = context.Canceled
		}
		if operationErr == nil {
			operationErr = ctx.Err()
		}
		if !returned || operationErr != nil {
			_ = o.retire()
		}
		cleanupErr = o.cleanupErr
	}()
	operationErr = run(ctx, o)
	returned = true
	return
}

// Connected reports whether this scope still owns a connected session. It is
// only a snapshot; cancellation or Close can retire the session immediately.
func (o *Operation) Connected() bool {
	o.scope.mu.Lock()
	defer o.scope.mu.Unlock()
	return o.scope.active && !o.retired.Load() && o.device.Connected()
}

// Retire closes the session without closing the Device. It is allowed after
// cancellation, but rejects reentry, repeated retirement and expired scopes.
// A subsequent explicit Device.Connect can establish a new session. Device.Close
// can retry failed cleanup after the scope returns.
func (o *Operation) Retire() error {
	_, leave, err := o.scope.enter(context.Background())
	if err != nil {
		return err
	}
	defer leave()
	if o.retired.Load() {
		return ErrScope
	}
	return o.retire()
}

// ExecuteCommand runs one command inside the operation.
func (o *Operation) ExecuteCommand(ctx context.Context, c Command, opts ExecuteOptions) (Result, error) {
	return o.ExecuteScript(ctx, Script{Commands: []Command{c}}, opts)
}

// ExecuteScript runs script inside the operation. It fails with ErrScope once
// the operation has ended.
func (o *Operation) ExecuteScript(ctx context.Context, s Script, opts ExecuteOptions) (Result, error) {
	script, e := s.clone()
	if e != nil {
		return Result{}, e
	}
	if opts.MaxOutputBytes < 0 || opts.PreserveSessionOnRejection && (!o.borrowed || len(script.Commands) != 1) {
		return Result{}, ErrInvalid
	}
	ctx, leave, e := o.scope.enter(ctx)
	if e != nil {
		return Result{}, e
	}
	defer leave()
	if o.retired.Load() {
		return Result{}, ErrScope
	}
	return o.device.execute(ctx, script, opts, nil)
}

// RunningConfig collects the running configuration inside the operation.
func (o *Operation) RunningConfig(ctx context.Context) (Result, error) {
	ctx, leave, e := o.scope.enter(ctx)
	if e != nil {
		return Result{}, e
	}
	defer leave()
	if o.retired.Load() {
		return Result{}, ErrScope
	}
	return o.device.runningConfig(ctx)
}

// SaveConfig runs the platform's save commands inside the operation and
// confirms them with the driver.
func (o *Operation) SaveConfig(ctx context.Context, opts ExecuteOptions) (Result, error) {
	if opts.PreserveSessionOnRejection {
		return Result{}, ErrInvalid
	}
	ctx, leave, err := o.scope.enter(ctx)
	if err != nil {
		return Result{}, err
	}
	defer leave()
	if o.retired.Load() {
		return Result{}, ErrScope
	}
	return o.device.saveConfig(ctx, opts)
}

// RawPrompt returns the device's last raw prompt.
func (o *Operation) RawPrompt() string { return o.device.RawPrompt() }

// Prompt returns the device's last rendered prompt.
func (o *Operation) Prompt() (string, error) { return o.device.Prompt() }

// PlatformName returns the device's platform name.
func (o *Operation) PlatformName() string { return o.device.PlatformName() }

// Host returns the device's host address.
func (o *Operation) Host() string { return o.device.Host() }

// retire runs under the operation's lease, either in one admitted call or after
// scope.end joins it. The first cleanup result survives every enclosing defer.
func (o *Operation) retire() error {
	if !o.retired.Swap(true) {
		o.cleanupErr = joinCleanup(nil, o.device.closeOwnedResources())
	}
	return o.cleanupErr
}
