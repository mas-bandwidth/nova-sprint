package run

// pushCollection is an isolated observation of a completed collection. The
// optional hook is used by the model trace fixture, never by the service.
type pushCollection struct {
	Recipient          string
	From, Through, End int64
	Epoch              int
	Batches            []pushBatch
}

func collectionSnapshot(recipient string, from, through int64, epoch int, end int64, batches []pushBatch) pushCollection {
	out := pushCollection{Recipient: recipient, From: from, Through: through, End: end, Epoch: epoch, Batches: make([]pushBatch, len(batches))}
	for i, b := range batches {
		out.Batches[i] = b
		out.Batches[i].lines = append([]string(nil), b.lines...)
	}
	return out
}
