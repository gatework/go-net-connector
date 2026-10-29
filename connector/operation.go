package connector

import "context"

// Operation lends sequential device methods to a single composite operation.
// It expires when WithOperation returns and never permits observer reentry.
type Operation struct {
	device *Device
	scope  *callScope
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
	defer func() {
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
func (o *Operation) ExecuteCommand(ctx context.Context, c Command, opts ExecuteOptions) (Result, error) {
	return o.ExecuteScript(ctx, Script{Commands: []Command{c}}, opts)
}
func (o *Operation) ExecuteScript(ctx context.Context, s Script, opts ExecuteOptions) (Result, error) {
	script, e := s.clone()
	if e != nil {
		return Result{}, e
	}
	if opts.MaxOutputBytes < 0 {
		return Result{}, ErrInvalid
	}
	ctx, leave, e := o.scope.enter(ctx)
	if e != nil {
		return Result{}, e
	}
	defer leave()
	return o.device.execute(ctx, script, opts, nil)
}
func (o *Operation) RunningConfig(ctx context.Context) (Result, error) {
	ctx, leave, e := o.scope.enter(ctx)
	if e != nil {
		return Result{}, e
	}
	defer leave()
	return o.device.runningConfig(ctx)
}
func (o *Operation) SaveConfig(ctx context.Context, opts ExecuteOptions) (Result, error) {
	ctx, leave, err := o.scope.enter(ctx)
	if err != nil {
		return Result{}, err
	}
	defer leave()
	return o.device.saveConfig(ctx, opts)
}

func (o *Operation) CurrentPrompt() string { return o.device.CurrentPrompt() }
func (o *Operation) ProfileName() string   { return o.device.ProfileName() }
func (o *Operation) Host() string          { return o.device.Host() }
