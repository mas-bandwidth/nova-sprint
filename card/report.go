package card

import "slices"

// ReportPlan is one plan for a row's report, read from the snapshot as it
// stands: the consumer's Report, and for a work consumer the producer's
// Finished, Broken or Faulted, composed by Merge. A read moves only the
// consumer (ReadYes and ReadFix belong to the tick). It adds no rule.
func ReportPlan(c, p Card, consumers []Card, rows map[string]Row, s Stream, needs map[string]Card, r Report) (Plan, *Refusal) {
	cons, err := MoveConsumer(c, p, rows[c.Holder], r)
	if err != nil {
		return Plan{}, err
	}
	if c.Kind != "work" {
		return cons, nil
	}
	ev, err := workFollow(c.ID, r.Result)
	if err != nil {
		return Plan{}, err
	}
	prod, err := Move(p, reportedCons(consumers, c, r), rows, s, needs, ev)
	if err != nil {
		return Plan{}, err
	}
	return Merge(cons, prod)
}

// workFollow is the producer move a work result asks for.
func workFollow(id, result string) (Event, *Refusal) {
	switch result {
	case "ok":
		return Finished{Consumer: id}, nil
	case "fail":
		return Broken{Consumer: id}, nil
	case "fault":
		return Faulted{Consumer: id}, nil
	default:
		return nil, no("a work consumer reports ok, fail or fault; %s is not one of them", result)
	}
}

// reportedCons is consumers with c as the report leaves it, so the producer
// move can be built; Merge runs that move again against the report's writes.
func reportedCons(consumers []Card, c Card, r Report) []Card {
	done := c
	done.State = Done
	done.Result = r.Result
	done.Reason = r.Reason
	if r.Head != "" {
		done.Head = r.Head
	}
	out := slices.Clone(consumers)
	i := slices.IndexFunc(out, func(x Card) bool { return x.ID == c.ID })
	if i < 0 {
		return append(out, done)
	}
	out[i] = done
	return out
}
