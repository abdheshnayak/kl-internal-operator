package operator

import "github.com/kloudlite/internal_operator_v2/lib/errors"

type fstring string

const (
	ErrNotInInputs        fstring = "key=%s not found in .Spec.Inputs"
	ErrNotInGeneratedVars fstring = "key=%s not found in .Status.GeneratedVars"
	ErrNotInDisplayVars   fstring = "key=%s not found in .Status.DisplayVars"
	ErrNotInReqLocals     fstring = "key=%s not found in req.Locals"
)

func (f fstring) Format(args ...string) error {
	return errors.Newf(string(f), args)
}
