package auth

import "fmt"

// SetBcryptCost overrides the runtime BcryptCost used by Hash. Call
// exactly once at server boot, BEFORE any goroutine that may invoke
// Hash starts; concurrent writes are not safe and there is no lock —
// Hash's read happens-before relies on the boot-then-listen ordering
// in main.go.
//
// Returns an error when c falls outside [MinBcryptCost, MaxBcryptCost].
// The floor enforces PRD §9 / OWASP guidance; the ceiling matches the
// hard upper bound bcrypt.GenerateFromPassword accepts (its source
// rejects costs above 31 with bcrypt.InvalidCostError).
func SetBcryptCost(c int) error {
	if c < MinBcryptCost || c > MaxBcryptCost {
		return fmt.Errorf("auth: bcrypt cost %d out of range [%d, %d]", c, MinBcryptCost, MaxBcryptCost)
	}
	BcryptCost = c
	return nil
}
