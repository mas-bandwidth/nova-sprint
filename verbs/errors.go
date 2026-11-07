package verbs

import "errors"

// OperationError is an adapter's deliberately safe, user-facing diagnosis.
// Fields must not contain credentials, raw output, or untrusted multiline text.
type OperationError struct {
	Problem, Fix string
	Exit         int
}

func (e *OperationError) Error() string { return "external operation failed" }

func operationalFailure(verb string, err error) Result {
	var named *OperationError
	if errors.As(err, &named) && named.Problem != "" && named.Fix != "" {
		r := refused(verb, named.Problem, named.Fix)
		if named.Exit == 2 {
			r.Exit = 2
		}
		return r
	}
	return unavailable(verb)
}
