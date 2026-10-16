package friend

// InPlace marks a Deliverer whose delivery holds no turn of the session (a
// file write): the delivery check runs it in place, never on its Go scheduler,
// so the check is delivered before ask returns and before any beat names it.
type InPlace interface {
	InPlace()
}
