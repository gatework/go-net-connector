package config

// FieldError identifies one invalid or required configuration field without
// retaining its potentially secret value.
type FieldError struct{ Field, Problem string }

func (e *FieldError) Error() string { return "config: " + e.Field + ": " + e.Problem }
func (e *FieldError) Unwrap() error { return ErrInvalid }
