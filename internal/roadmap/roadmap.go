// Package roadmap reads, edits and checks a roadmap s-expression
// (roadmaps/*.sexp, docs/SPEC-WORK-V1.md section 1.12). The parser keeps
// every byte (sexp.go), so an unedited file prints back byte for byte, and an
// edit changes only the nodes it touches. Add, Remove, Pull and Note find
// every card they name before they change anything; Check and CheckBytes are
// what the nova-work roadmap verbs hold a file to before they save it.
package roadmap

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/atomicfile"
)

type Card struct {
	ID        string
	Tier      string
	Needs     []string
	Title     string
	Brief     string
	Node      *Node
	BriefNode *Node
	Parent    *Stream
}

type Stream struct {
	Name      string
	Cards     []*Card
	Node      *Node
	CardsNode *Node
	Parent    *Release
}

type Release struct {
	Version     string
	CardsCount  int
	Streams     []*Stream
	Node        *Node
	CountNode   *Node
	StreamsNode *Node
	Parent      *Roadmap
}

type Roadmap struct {
	Doc          *Document
	Path         string
	Releases     []*Release
	ReleasesNode *Node
}

func Load(path string) (*Roadmap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	r, err := LoadBytes(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	r.Path = path
	return r, nil
}

func LoadBytes(data []byte) (*Roadmap, error) {
	doc, err := Parse(data)
	if err != nil {
		return nil, err
	}
	return parseRoadmap(doc)
}

func parseRoadmap(doc *Document) (*Roadmap, error) {
	root := doc.Root
	if root == nil || root.Kind != NodeList || len(root.Children) == 0 {
		return nil, fmt.Errorf("roadmap must be a non-empty list starting with :roadmap")
	}
	first := root.Children[0]
	if first.Kind != NodeAtom || (first.Value != ":roadmap" && first.Value != "roadmap") {
		return nil, fmt.Errorf("expected :roadmap form, got %s", first.Value)
	}

	rm := &Roadmap{Doc: doc}

	// Locate :releases
	for i := 1; i < len(root.Children); i++ {
		ch := root.Children[i]
		if ch.Kind == NodeAtom && (ch.Value == ":releases" || ch.Value == "releases") {
			if i+1 >= len(root.Children) || root.Children[i+1].Kind != NodeList {
				return nil, fmt.Errorf(":releases must be followed by a list of releases")
			}
			rm.ReleasesNode = root.Children[i+1]
			break
		}
	}
	if rm.ReleasesNode == nil {
		return nil, fmt.Errorf("missing :releases in roadmap")
	}

	for _, relNode := range rm.ReleasesNode.Children {
		if relNode.Kind != NodeList || len(relNode.Children) == 0 {
			continue
		}
		firstRel := relNode.Children[0]
		if firstRel.Kind != NodeAtom || (firstRel.Value != ":release" && firstRel.Value != "release") {
			continue
		}
		rel := &Release{
			Node:   relNode,
			Parent: rm,
		}

		// Parse release fields
		for j := 1; j < len(relNode.Children); j++ {
			c := relNode.Children[j]
			if j == 1 && c.Kind == NodeString {
				rel.Version = c.Value
				continue
			}
			if c.Kind == NodeAtom && (c.Value == ":version" || c.Value == "version") && j+1 < len(relNode.Children) {
				rel.Version = relNode.Children[j+1].Value
				j++
				continue
			}
			if c.Kind == NodeAtom && (c.Value == ":cards" || c.Value == "cards") && j+1 < len(relNode.Children) {
				countNode := relNode.Children[j+1]
				rel.CountNode = countNode
				n, err := strconv.Atoi(countNode.Value)
				if err == nil {
					rel.CardsCount = n
				}
				j++
				continue
			}
			if c.Kind == NodeAtom && (c.Value == ":streams" || c.Value == "streams") && j+1 < len(relNode.Children) {
				streamsNode := relNode.Children[j+1]
				if streamsNode.Kind == NodeList {
					rel.StreamsNode = streamsNode
					for _, strNode := range streamsNode.Children {
						if strNode.Kind != NodeList || len(strNode.Children) == 0 {
							continue
						}
						stream, err := parseStream(strNode, rel)
						if err != nil {
							return nil, err
						}
						rel.Streams = append(rel.Streams, stream)
					}
				}
				j++
				continue
			}
		}
		rm.Releases = append(rm.Releases, rel)
	}

	return rm, nil
}

func parseStream(node *Node, rel *Release) (*Stream, error) {
	s := &Stream{
		Node:   node,
		Parent: rel,
	}
	for i := 1; i < len(node.Children); i++ {
		c := node.Children[i]
		if i == 1 && c.Kind == NodeString {
			s.Name = c.Value
			continue
		}
		if c.Kind == NodeAtom && (c.Value == ":name" || c.Value == "name") && i+1 < len(node.Children) {
			s.Name = node.Children[i+1].Value
			i++
			continue
		}
		if c.Kind == NodeAtom && (c.Value == ":cards" || c.Value == "cards") && i+1 < len(node.Children) {
			cardsNode := node.Children[i+1]
			if cardsNode.Kind == NodeList {
				s.CardsNode = cardsNode
				for _, cardNode := range cardsNode.Children {
					if cardNode.Kind != NodeList {
						continue
					}
					card := parseCard(cardNode, s)
					s.Cards = append(s.Cards, card)
				}
			}
			i++
			continue
		}
	}
	return s, nil
}

func parseCard(node *Node, stream *Stream) *Card {
	card := &Card{
		Node:   node,
		Parent: stream,
		Tier:   "-",
	}
	for i := 0; i < len(node.Children); i++ {
		c := node.Children[i]
		if c.Kind != NodeAtom {
			continue
		}
		key := c.Value
		if strings.HasPrefix(key, ":") {
			key = key[1:]
		}
		if i+1 >= len(node.Children) {
			break
		}
		valNode := node.Children[i+1]
		switch key {
		case "id":
			card.ID = valNode.Value
			i++
		case "tier":
			card.Tier = valNode.Value
			i++
		case "title":
			card.Title = valNode.Value
			i++
		case "brief":
			card.Brief = valNode.Value
			card.BriefNode = valNode
			i++
		case "needs":
			if valNode.Kind == NodeList {
				for _, needNode := range valNode.Children {
					card.Needs = append(card.Needs, needNode.Value)
				}
			}
			i++
		}
	}
	return card
}

func (r *Roadmap) Bytes() []byte {
	return r.Doc.Print()
}

func (r *Roadmap) Save(path string) error {
	return atomicfile.Write(filepath.Clean(path), r.Bytes(), 0644)
}

func (r *Roadmap) Check() []string {
	var problems []string
	seenIDs := make(map[string]bool)

	for _, rel := range r.Releases {
		actualCards := 0
		for _, s := range rel.Streams {
			if len(s.Cards) == 0 {
				problems = append(problems, fmt.Sprintf("empty stream %q in release %s", s.Name, rel.Version))
			}
			for _, card := range s.Cards {
				actualCards++
				if seenIDs[card.ID] {
					problems = append(problems, fmt.Sprintf("duplicate card id %q", card.ID))
				}
				seenIDs[card.ID] = true
			}
		}
		if rel.CardsCount != actualCards {
			problems = append(problems, fmt.Sprintf("release %s count mismatch: :cards is %d, actual count is %d",
				rel.Version, rel.CardsCount, actualCards))
		}
	}
	return problems
}

// CheckFile reads path and checks it (CheckBytes); only a read error is an
// error: an unbalanced or unparsable file is a problem, like a count mismatch.
func CheckFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return CheckBytes(data), nil
}

// CheckBytes is every problem of a roadmap's bytes: unbalanced or unparsable
// (string-aware), then Check's. A verb checks the bytes it is about to save,
// so it never writes a file that check rejects.
func CheckBytes(data []byte) []string {
	doc, err := Parse(data)
	if err != nil {
		return []string{err.Error()}
	}
	r, err := parseRoadmap(doc)
	if err != nil {
		return []string{err.Error()}
	}
	return r.Check()
}

// Find is the card with id in any release, or nil.
func (r *Roadmap) Find(id string) *Card {
	for _, rel := range r.Releases {
		for _, s := range rel.Streams {
			for _, c := range s.Cards {
				if c.ID == id {
					return c
				}
			}
		}
	}
	return nil
}

// find is the card of every id, or an error naming the first id the roadmap
// does not hold; it changes nothing, so a verb refuses before it edits.
func (r *Roadmap) find(ids []string) ([]*Card, error) {
	var cards []*Card
	for _, id := range ids {
		c := r.Find(id)
		if c == nil {
			return nil, fmt.Errorf("card %q not found in roadmap", id)
		}
		cards = append(cards, c)
	}
	return cards, nil
}

func (r *Roadmap) Remove(ids ...string) (int, error) {
	if _, err := r.find(ids); err != nil {
		return 0, err
	}
	toRemove := make(map[string]bool)
	for _, id := range ids {
		toRemove[id] = true
	}

	found := make(map[string]bool)
	totalRemoved := 0

	for _, rel := range r.Releases {
		relRemoved := 0
		var remainingStreams []*Stream
		var remainingStreamNodes []*Node

		for _, s := range rel.Streams {
			var remainingCards []*Card
			var remainingCardNodes []*Node

			for _, c := range s.Cards {
				if toRemove[c.ID] {
					found[c.ID] = true
					relRemoved++
					totalRemoved++
				} else {
					remainingCards = append(remainingCards, c)
					remainingCardNodes = append(remainingCardNodes, c.Node)
				}
			}

			s.Cards = remainingCards
			if s.CardsNode != nil {
				s.CardsNode.Children = remainingCardNodes
			}

			if len(s.Cards) > 0 {
				remainingStreams = append(remainingStreams, s)
				remainingStreamNodes = append(remainingStreamNodes, s.Node)
			}
		}

		rel.Streams = remainingStreams
		if rel.StreamsNode != nil {
			rel.StreamsNode.Children = remainingStreamNodes
		}

		if relRemoved > 0 {
			rel.CardsCount -= relRemoved
			if rel.CountNode != nil {
				rel.CountNode.Value = strconv.Itoa(rel.CardsCount)
			}
		}
	}

	var missing []string
	for _, id := range ids {
		if !found[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		return totalRemoved, fmt.Errorf("card %q not found in roadmap", missing[0])
	}

	return totalRemoved, nil
}

// Pull writes each card's brief to <outDir>/<id>.md and removes the cards.
// Every id is found, and every brief is non-empty, before anything is
// written: a card with no brief has nothing for nova-sprint add to read.
func (r *Roadmap) Pull(outDir string, ids ...string) ([]*Card, error) {
	matchedCards, err := r.find(ids)
	if err != nil {
		return nil, err
	}
	for _, c := range matchedCards {
		if strings.TrimSpace(c.Brief) == "" {
			return nil, fmt.Errorf("card %q has an empty :brief; nothing to write for it", c.ID)
		}
	}

	if err := os.MkdirAll(outDir, 0755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", outDir, err)
	}

	for _, c := range matchedCards {
		filePath := filepath.Join(outDir, c.ID+".md")
		content := c.Brief
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		if err := atomicfile.Write(filepath.Clean(filePath), []byte(content), 0644); err != nil {
			return nil, fmt.Errorf("write %s: %w", filePath, err)
		}
	}

	if _, err := r.Remove(ids...); err != nil {
		return nil, err
	}

	return matchedCards, nil
}

func (r *Roadmap) Note(text string, ids ...string) (int, error) {
	if _, err := r.find(ids); err != nil {
		return 0, err
	}
	idMap := make(map[string]bool)
	for _, id := range ids {
		idMap[id] = true
	}

	count := 0
	found := make(map[string]bool)

	for _, rel := range r.Releases {
		for _, s := range rel.Streams {
			for _, c := range s.Cards {
				if idMap[c.ID] {
					found[c.ID] = true
					count++
					newBrief := strings.TrimRight(c.Brief, "\n") + "\n\n" + text + "\n"
					c.Brief = newBrief
					if c.BriefNode != nil {
						c.BriefNode.Value = newBrief
						c.BriefNode.Raw = QuoteString(newBrief)
					}
				}
			}
		}
	}

	var missing []string
	for _, id := range ids {
		if !found[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		return count, fmt.Errorf("card %q not found in roadmap", missing[0])
	}

	return count, nil
}

func (r *Roadmap) Add(streamName, briefDir string) (int, error) {
	entries, err := os.ReadDir(briefDir)
	if err != nil {
		return 0, fmt.Errorf("read brief-dir %s: %w", briefDir, err)
	}

	var mdFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			mdFiles = append(mdFiles, e.Name())
		}
	}
	if len(mdFiles) == 0 {
		return 0, fmt.Errorf("no *.md brief files found in %s", briefDir)
	}

	sort.Strings(mdFiles)

	if len(r.Releases) == 0 {
		return 0, fmt.Errorf("no releases in roadmap")
	}
	if rel := r.Releases[0]; rel.StreamsNode == nil {
		return 0, fmt.Errorf("release %s has no :streams list", rel.Version)
	}

	// Every brief is read, and every id checked against every release, before
	// the roadmap changes: a refusal leaves it as it was.
	contents := make([]string, len(mdFiles))
	for i, fileName := range mdFiles {
		id := strings.TrimSuffix(fileName, ".md")
		if c := r.Find(id); c != nil {
			release := ""
			if c.Parent != nil && c.Parent.Parent != nil {
				release = c.Parent.Parent.Version
			}
			return 0, fmt.Errorf("card %q is already in the roadmap (release %s, stream %s); remove it or rename %s",
				id, release, c.Parent.Name, filepath.Join(briefDir, fileName))
		}
		data, err := os.ReadFile(filepath.Join(briefDir, fileName))
		if err != nil {
			return 0, fmt.Errorf("read %s: %w", filepath.Join(briefDir, fileName), err)
		}
		if strings.TrimSpace(string(data)) == "" {
			return 0, fmt.Errorf("brief %s is empty", filepath.Join(briefDir, fileName))
		}
		contents[i] = string(data)
	}
	rel := r.Releases[0] // Add to the active (first) release

	// Find or create stream
	var targetStream *Stream
	for _, s := range rel.Streams {
		if s.Name == streamName {
			targetStream = s
			break
		}
	}

	if targetStream == nil {
		cardsListNode := &Node{
			Kind:     NodeList,
			Leading:  "\n     ",
			Closing:  "\n     ",
			Children: []*Node{},
		}
		streamNode := &Node{
			Kind:    NodeList,
			Leading: "\n   ",
			Closing: "",
			Children: []*Node{
				{Kind: NodeAtom, Leading: "", Value: ":stream"},
				{Kind: NodeString, Leading: " ", Value: streamName, Raw: QuoteString(streamName)},
				{Kind: NodeAtom, Leading: " ", Value: ":cards"},
				cardsListNode,
			},
		}
		rel.StreamsNode.Children = append(rel.StreamsNode.Children, streamNode)
		targetStream = &Stream{
			Name:      streamName,
			Node:      streamNode,
			CardsNode: cardsListNode,
			Parent:    rel,
		}
		rel.Streams = append(rel.Streams, targetStream)
	}

	added := 0
	for i, fileName := range mdFiles {
		id := strings.TrimSuffix(fileName, ".md")
		content := contents[i]

		title, tier, needs := parseBriefMetadata(content, id)

		var needNodes []*Node
		for _, need := range needs {
			needNodes = append(needNodes, &Node{
				Kind:    NodeString,
				Leading: " ",
				Value:   need,
				Raw:     QuoteString(need),
			})
		}
		if len(needNodes) > 0 {
			needNodes[0].Leading = ""
		}
		needsListNode := &Node{
			Kind:     NodeList,
			Leading:  " ",
			Children: needNodes,
		}

		briefNode := &Node{
			Kind:    NodeString,
			Leading: " ",
			Value:   content,
			Raw:     QuoteString(content),
		}

		cardNode := &Node{
			Kind:    NodeList,
			Leading: "\n     ",
			Children: []*Node{
				{Kind: NodeAtom, Leading: "", Value: ":id"},
				{Kind: NodeString, Leading: " ", Value: id, Raw: QuoteString(id)},
				{Kind: NodeAtom, Leading: " ", Value: ":tier"},
				{Kind: NodeString, Leading: " ", Value: tier, Raw: QuoteString(tier)},
				{Kind: NodeAtom, Leading: " ", Value: ":needs"},
				needsListNode,
				{Kind: NodeAtom, Leading: "\n      ", Value: ":title"},
				{Kind: NodeString, Leading: " ", Value: title, Raw: QuoteString(title)},
				{Kind: NodeAtom, Leading: "\n      ", Value: ":brief"},
				briefNode,
			},
		}

		targetStream.CardsNode.Children = append(targetStream.CardsNode.Children, cardNode)
		newCard := &Card{
			ID:        id,
			Tier:      tier,
			Needs:     needs,
			Title:     title,
			Brief:     content,
			Node:      cardNode,
			BriefNode: briefNode,
			Parent:    targetStream,
		}
		targetStream.Cards = append(targetStream.Cards, newCard)
		added++
	}

	rel.CardsCount += added
	if rel.CountNode != nil {
		rel.CountNode.Value = strconv.Itoa(rel.CardsCount)
	}

	return added, nil
}

func parseBriefMetadata(brief, fallbackID string) (title, tier string, needs []string) {
	tier = "-"
	lines := strings.Split(brief, "\n")
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "TIER:") {
			v := strings.TrimSpace(strings.TrimPrefix(trimmed, "TIER:"))
			if v != "" {
				tier = v
			}
		} else if strings.HasPrefix(trimmed, "DEPENDS-ON:") {
			v := strings.TrimSpace(strings.TrimPrefix(trimmed, "DEPENDS-ON:"))
			if v != "" && v != "none" {
				for _, part := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' }) {
					part = strings.TrimSpace(part)
					if part != "" {
						needs = append(needs, part)
					}
				}
			}
		} else if strings.HasPrefix(trimmed, "NEEDS:") {
			v := strings.TrimSpace(strings.TrimPrefix(trimmed, "NEEDS:"))
			if v != "" && v != "none" {
				for _, part := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' }) {
					part = strings.TrimSpace(part)
					if part != "" {
						needs = append(needs, part)
					}
				}
			}
		}
	}

	// Extract title from "THE TASK."
	if idx := strings.Index(brief, "THE TASK."); idx >= 0 {
		rest := strings.TrimSpace(brief[idx+len("THE TASK."):])
		// Take first sentence: up to ". " or ".\n" or quote or 200 chars
		dotIdx := strings.Index(rest, ". ")
		nlIdx := strings.Index(rest, ".\n")
		end := -1
		if dotIdx >= 0 && nlIdx >= 0 {
			end = min(dotIdx, nlIdx)
		} else if dotIdx >= 0 {
			end = dotIdx
		} else if nlIdx >= 0 {
			end = nlIdx
		}

		if end > 0 {
			title = strings.TrimSpace(rest[:end])
		} else {
			// fallback to first non-empty line
			firstLine := strings.Split(rest, "\n")[0]
			title = strings.TrimSpace(firstLine)
		}
		if len(title) > 200 {
			title = title[:200]
		}
	}

	if title == "" {
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if l != "" && !strings.Contains(l, ":") {
				title = l
				break
			}
		}
	}
	if title == "" {
		title = fallbackID
	}

	return title, tier, needs
}
