package connector

import "context"

// Driver is one platform's device driver: its terminal grammar plus the
// behavior that grammar cannot declare. Vendor platform objects implement it.
type Driver interface {
	// Terminal returns an independent copy of the platform grammar.
	Terminal() Terminal
	// NewConfigCollector returns a fresh strategy for one configuration
	// collection, or nil to use the generic rendered-output collector.
	NewConfigCollector() ConfigStrategy
	// ValidateSave requires affirmative device evidence that the save
	// completed; a prompt alone is not proof. It returns ErrUnconfirmed
	// (or a wrapped cause) when the evidence is missing.
	ValidateSave(ctx context.Context, result Result) error
}

// CanCollectConfig reports whether d declares a configuration collection sequence.
func CanCollectConfig(d Driver) bool { return len(d.Terminal().ConfigCommands) > 0 }

// CanSave reports whether d declares a save sequence. A successful save still
// needs the affirmative evidence checked by ValidateSave.
func CanSave(d Driver) bool { return len(d.Terminal().SaveCommands) > 0 }

// CommandPreparer is an optional Driver hook that may rewrite or skip a
// command before it is sent, for example to substitute a device-assigned id.
type CommandPreparer interface {
	PrepareCommand(ctx context.Context, command Command, executor Executor) (cmd Command, skip bool, err error)
}

// CommandObserver is an optional Driver hook run after a command is accepted.
type CommandObserver interface {
	AfterCommand(ctx context.Context, command Command, step StepResult, executor Executor) error
}

// LoginInspector is an optional Driver hook that reads device state from the
// login banner, such as a high-availability role.
type LoginInspector interface {
	AfterLogin(ctx context.Context, step StepResult) (LoginState, error)
}

// LoginCommitter is an optional transport capability that persists
// authentication evidence, such as a newly trusted host key, once login
// completed. It receives the admitted login context and must synchronize with
// Close.
type LoginCommitter interface {
	Authenticated(ctx context.Context) error
}

// StaticDriver adapts a Terminal and optional functions into a Driver. Nil
// functions mean: generic rendered configuration collection, saves are never
// confirmed, and no command or login hooks.
type StaticDriver struct {
	Grammar      Terminal
	Collector    func() ConfigStrategy
	Save         func(context.Context, Result) error
	Prepare      func(context.Context, Command, Executor) (Command, bool, error)
	AfterCommand func(context.Context, Command, StepResult, Executor) error
	AfterLogin   func(context.Context, StepResult) (LoginState, error)
}

// Terminal returns the configured grammar; Device construction clones it.
func (s StaticDriver) Terminal() Terminal { return s.Grammar }

// NewConfigCollector creates a per-collection strategy, or selects generic collection when unset.
func (s StaticDriver) NewConfigCollector() ConfigStrategy {
	if s.Collector == nil {
		return nil
	}
	return s.Collector()
}

// ValidateSave runs the configured confirmation check or returns ErrUnconfirmed.
func (s StaticDriver) ValidateSave(ctx context.Context, result Result) error {
	if s.Save == nil {
		return ErrUnconfirmed
	}
	return s.Save(ctx, result)
}

// hooks exposes StaticDriver functions as optional hooks only when set,
// so an unset hook behaves exactly like a driver without that interface.
func (s StaticDriver) hooks() (CommandPreparer, CommandObserver, LoginInspector) {
	var p CommandPreparer
	var o CommandObserver
	var l LoginInspector
	if s.Prepare != nil {
		p = preparerFunc(s.Prepare)
	}
	if s.AfterCommand != nil {
		o = observerFunc(s.AfterCommand)
	}
	if s.AfterLogin != nil {
		l = inspectorFunc(s.AfterLogin)
	}
	return p, o, l
}

type preparerFunc func(context.Context, Command, Executor) (Command, bool, error)

func (f preparerFunc) PrepareCommand(ctx context.Context, c Command, x Executor) (Command, bool, error) {
	return f(ctx, c, x)
}

type observerFunc func(context.Context, Command, StepResult, Executor) error

func (f observerFunc) AfterCommand(ctx context.Context, c Command, s StepResult, x Executor) error {
	return f(ctx, c, s, x)
}

type inspectorFunc func(context.Context, StepResult) (LoginState, error)

func (f inspectorFunc) AfterLogin(ctx context.Context, s StepResult) (LoginState, error) {
	return f(ctx, s)
}

// WithTerminal returns a driver that keeps d's behavior but uses grammar t,
// for callers that customize a platform's prompts or commands.
//
//nolint:iface // a decorator returns the interface it decorates
func WithTerminal(d Driver, t Terminal) Driver { return terminalOverride{Driver: d, grammar: t} }

type terminalOverride struct {
	Driver
	grammar Terminal
}

func (o terminalOverride) Terminal() Terminal { return o.grammar }

// hookSource lets an adapter report its own hooks instead of exposing them
// as methods it may not actually provide.
type hookSource interface {
	hooks() (CommandPreparer, CommandObserver, LoginInspector)
}

func (o terminalOverride) hooks() (CommandPreparer, CommandObserver, LoginInspector) {
	return driverHooks(o.Driver)
}

// driverHooks resolves the optional hooks once, at Device construction.
func driverHooks(d Driver) (CommandPreparer, CommandObserver, LoginInspector) {
	if source, ok := d.(hookSource); ok {
		return source.hooks()
	}
	p, _ := d.(CommandPreparer)
	o, _ := d.(CommandObserver)
	l, _ := d.(LoginInspector)
	return p, o, l
}
