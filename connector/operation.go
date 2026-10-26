package connector

import "context"

// Operation lends sequential device methods to a single composite operation.
// It expires when WithOperation returns and never permits observer reentry.
type Operation struct {
	device *Device
	scope  *callScope
}

func (d *Device) WithOperation(ctx context.Context, run func(*Operation) error) (err error) {
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
	o := &Operation{device: d, scope: newCallScope(ctx)}
	defer func() {
		if o.scope.end() && err == nil {
			err = context.Canceled
		}
		if err != nil {
			_ = d.closeResources()
		}
	}()
	err = run(o)
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
	if len(o.device.profile.SaveCommands) == 0 {
		return Result{}, ErrUnsupported
	}
	var commands []Command
	for _, text := range o.device.profile.SaveCommands {
		commands = append(commands, Command{Text: text})
	}
	return o.ExecuteScript(ctx, Script{Commands: commands}, opts)
}
func (o *Operation) CurrentPrompt() string { return o.device.CurrentPrompt() }
func (o *Operation) Name() string          { return o.device.Name() }
func (o *Operation) Host() string          { return o.device.Host() }
