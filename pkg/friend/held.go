package friend

// What a friend holds, as the sprint server answers friend cards <friend> (the daemon that
// writes the friend's inbox from the answer is nova-tools' nova-friend run).

// HeldCard is one card on the friend's row as the server answers it: the card, the job it
// is delivered as (inbox/<job>), its column (ready or working) and its BRIEF.md whole, with
// its packet (its kind, work or read, the branch it is pushed to, its tier, attempt,
// generation and epoch, and the repository and base its checkout is staged from); Why is,
// for a card the answer sends no brief for, why not.
type HeldCard struct {
	Card    string `json:"card"`
	Job     string `json:"job"`
	Col     string `json:"col"`
	Kind    string `json:"kind,omitempty"`
	Branch  string `json:"branch,omitempty"`
	Tier    string `json:"tier,omitempty"`
	Stream  string `json:"stream,omitempty"`
	Attempt int    `json:"attempt,omitempty"`
	Gen     int    `json:"gen,omitempty"`
	Epoch   uint64 `json:"epoch"`
	Repo    string `json:"repo,omitempty"`
	Base    string `json:"base,omitempty"`
	Brief   string `json:"brief"`
	// Lane is the lane of her row that holds the card on the server (take --lane), 0 for none.
	Lane int    `json:"lane,omitempty"`
	Why  string `json:"-"`
}

// HeldAnswer is the server's answer to friend cards <friend>: every card on her row, the
// ready ones dealt behind her working ones and her reads among them.
type HeldAnswer struct {
	Friend string     `json:"friend"`
	Cards  []HeldCard `json:"cards"`
}
