package config

// FieldError names a failing field without retaining its potentially secret value.
// FieldError identifies one invalid, required or removed configuration field.
// Replacement names the field to use instead when Problem is "removed".
type FieldError struct{ Field, Problem, Replacement string }

func (e *FieldError) Error() string { return "config: " + e.Field + ": " + e.Problem }
func (e *FieldError) Unwrap() error { return ErrInvalid }
