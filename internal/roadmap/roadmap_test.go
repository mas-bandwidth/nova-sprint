package roadmap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sampleRoadmap = `; sample roadmap comment
(:roadmap 1 :repo "mas-bandwidth/nova-sprint" :product "nova-sprint"
 :releases
 ((:release "v1.1.0" :status :planned :cards 3
   :streams
   ((:stream "stream-a" :cards
     ((:id "card-1" :tier "-" :needs ()
       :title "Title of card 1"
       :brief "Brief 1 text")
      (:id "card-2" :tier "1" :needs ("dep-0")
       :title "Title of card 2"
       :brief "Brief 2 text")))
    (:stream "stream-b" :cards
     ((:id "card-3" :tier "pro" :needs ()
       :title "Title of card 3"
       :brief "Brief 3 text")))))))
`

func TestRemoveKeepsTheRoadmapBalancedAndCounted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "roadmap.sexp")
	require.NoError(t, os.WriteFile(path, []byte(sampleRoadmap), 0644))

	r, err := Load(path)
	require.NoError(t, err)

	// Initial check should pass with 0 problems
	problems := r.Check()
	require.Empty(t, problems, "initial check must pass")

	// 1. Remove card-1 from stream-a (which has 2 cards).
	// Count should decrement from 3 to 2. stream-a should still exist with card-2.
	removed, err := r.Remove("card-1")
	require.NoError(t, err)
	assert.Equal(t, 1, removed)
	assert.Equal(t, 2, r.Releases[0].CardsCount)
	assert.Len(t, r.Releases[0].Streams, 2)
	assert.Len(t, r.Releases[0].Streams[0].Cards, 1)
	assert.Equal(t, "card-2", r.Releases[0].Streams[0].Cards[0].ID)

	require.NoError(t, r.Save(path))

	// Re-load and verify paren balance and counts
	r2, err := Load(path)
	require.NoError(t, err)
	assert.Empty(t, r2.Check())
	assert.Equal(t, 2, r2.Releases[0].CardsCount)

	// 2. Remove card-3 from stream-b (which has only 1 card).
	// stream-b should be dropped because it is now empty.
	// Count should decrement from 2 to 1.
	removed, err = r2.Remove("card-3")
	require.NoError(t, err)
	assert.Equal(t, 1, removed)
	assert.Equal(t, 1, r2.Releases[0].CardsCount)
	assert.Len(t, r2.Releases[0].Streams, 1, "stream-b should be dropped when emptied")
	assert.Equal(t, "stream-a", r2.Releases[0].Streams[0].Name)

	require.NoError(t, r2.Save(path))

	// Re-load and verify paren balance and counts
	r3, err := Load(path)
	require.NoError(t, err)
	assert.Empty(t, r3.Check())
	assert.Equal(t, 1, r3.Releases[0].CardsCount)

	// Ensure comments and structure remain balanced
	savedBytes, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(savedBytes), "; sample roadmap comment"))
	assert.Contains(t, string(savedBytes), ":cards 1")
	assert.NotContains(t, string(savedBytes), "stream-b")
	assert.NotContains(t, string(savedBytes), `:id "card-1"`)
	assert.NotContains(t, string(savedBytes), `:id "card-3"`)
	assert.Contains(t, string(savedBytes), `((:id "card-2"`, "the list opens as it did when its first card went")

	// 3. Pull the last card (named twice): its brief is written once, stream-a
	// goes, and the release is counted at 0 with an empty streams list.
	outDir := filepath.Join(dir, "briefs")
	pulled, err := r3.Pull(outDir, "card-2", "card-2")
	require.NoError(t, err)
	require.Len(t, pulled, 1)
	brief, err := os.ReadFile(filepath.Join(outDir, "card-2.md"))
	require.NoError(t, err)
	assert.Equal(t, "Brief 2 text\n", string(brief))
	assert.Equal(t, 0, r3.Releases[0].CardsCount)
	assert.Empty(t, r3.Releases[0].Streams)
	assert.Empty(t, CheckBytes(r3.Bytes()))
	assert.Contains(t, string(r3.Bytes()), ":cards 0")

	// 4. Check finds a count that no longer matches its streams.
	miscounted := strings.Replace(string(savedBytes), ":cards 1", ":cards 2", 1)
	problems = CheckBytes([]byte(miscounted))
	require.Len(t, problems, 1)
	assert.Contains(t, problems[0], "count mismatch: :cards is 2, actual count is 1")
}

func TestPullWritesBriefsAndRemovesCards(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "roadmap.sexp")
	require.NoError(t, os.WriteFile(path, []byte(sampleRoadmap), 0644))

	r, err := Load(path)
	require.NoError(t, err)

	outDir := filepath.Join(dir, "briefs")
	pulled, err := r.Pull(outDir, "card-1", "card-3")
	require.NoError(t, err)
	assert.Len(t, pulled, 2)

	// Check that card-1.md and card-3.md were written
	b1, err := os.ReadFile(filepath.Join(outDir, "card-1.md"))
	require.NoError(t, err)
	assert.Equal(t, "Brief 1 text\n", string(b1))

	b3, err := os.ReadFile(filepath.Join(outDir, "card-3.md"))
	require.NoError(t, err)
	assert.Equal(t, "Brief 3 text\n", string(b3))

	// Check that cards were removed from roadmap and stream-b was dropped
	assert.Equal(t, 1, r.Releases[0].CardsCount)
	assert.Len(t, r.Releases[0].Streams, 1)
	assert.Equal(t, "card-2", r.Releases[0].Streams[0].Cards[0].ID)

	require.NoError(t, r.Save(path))

	r2, err := Load(path)
	require.NoError(t, err)
	assert.Empty(t, r2.Check())
}

func TestCheckFindsCountMismatchAndEmptyStreams(t *testing.T) {
	// Count mismatch
	mismatched := strings.Replace(sampleRoadmap, ":cards 3", ":cards 5", 1)
	r, err := LoadBytes([]byte(mismatched))
	require.NoError(t, err)
	problems := r.Check()
	require.NotEmpty(t, problems)
	assert.Contains(t, problems[0], "count mismatch")

	// Duplicate card ID
	dupRoadmap := strings.Replace(sampleRoadmap, `"card-3"`, `"card-1"`, 1)
	rDup, err := LoadBytes([]byte(dupRoadmap))
	require.NoError(t, err)
	problemsDup := rDup.Check()
	require.NotEmpty(t, problemsDup)
	assert.Contains(t, problemsDup[0], "duplicate card id")

	// Unbalanced paren
	unbalanced := sampleRoadmap[:len(sampleRoadmap)-3]
	_, err = LoadBytes([]byte(unbalanced))
	require.Error(t, err)
}

func TestAddBriefsToStream(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "roadmap.sexp")
	require.NoError(t, os.WriteFile(path, []byte(sampleRoadmap), 0644))

	briefDir := filepath.Join(dir, "incoming")
	require.NoError(t, os.MkdirAll(briefDir, 0755))

	b1 := `REPO: mas-bandwidth/nova-tools
TIER: flash
DEPENDS-ON: card-1

THE TASK. First sentence of new card 4. Second sentence.
`
	b2 := `REPO: mas-bandwidth/nova-tools
TIER: pro

THE TASK. New card 5 task sentence.
`
	require.NoError(t, os.WriteFile(filepath.Join(briefDir, "card-4.md"), []byte(b1), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(briefDir, "card-5.md"), []byte(b2), 0644))

	r, err := Load(path)
	require.NoError(t, err)

	added, err := r.Add("new-stream", briefDir)
	require.NoError(t, err)
	assert.Equal(t, 2, added)

	// Release count should have increased from 3 to 5
	assert.Equal(t, 5, r.Releases[0].CardsCount)
	assert.Len(t, r.Releases[0].Streams, 3)

	require.NoError(t, r.Save(path))

	r2, err := Load(path)
	require.NoError(t, err)
	assert.Empty(t, r2.Check())
	assert.Equal(t, 5, r2.Releases[0].CardsCount)

	// Verify card-4 metadata
	var c4 *Card
	for _, s := range r2.Releases[0].Streams {
		if s.Name == "new-stream" {
			for _, c := range s.Cards {
				if c.ID == "card-4" {
					c4 = c
				}
			}
		}
	}
	require.NotNil(t, c4)
	assert.Equal(t, "flash", c4.Tier)
	assert.Equal(t, []string{"card-1"}, c4.Needs)
	assert.Equal(t, "First sentence of new card 4", c4.Title)
}

func TestNoteAppendsToBrief(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "roadmap.sexp")
	require.NoError(t, os.WriteFile(path, []byte(sampleRoadmap), 0644))

	r, err := Load(path)
	require.NoError(t, err)

	noted, err := r.Note("Reading note: review this later.", "card-1", "card-2")
	require.NoError(t, err)
	assert.Equal(t, 2, noted)

	require.NoError(t, r.Save(path))

	r2, err := Load(path)
	require.NoError(t, err)
	assert.Empty(t, r2.Check())

	var c1 *Card
	for _, c := range r2.Releases[0].Streams[0].Cards {
		if c.ID == "card-1" {
			c1 = c
		}
	}
	require.NotNil(t, c1)
	assert.Contains(t, c1.Brief, "Reading note: review this later.")
}

func TestRoundTripByteStable(t *testing.T) {
	doc, err := Parse([]byte(sampleRoadmap))
	require.NoError(t, err)
	printed := doc.Print()
	assert.Equal(t, sampleRoadmap, string(printed), "unmutated round trip must be byte identical")
}

func TestTheRoadmapShapeRoundTripsByteForByte(t *testing.T) {
	path := filepath.Join("testdata", "synthetic-v1.1.0.sexp")
	original, err := os.ReadFile(path)
	require.NoError(t, err)

	r, err := Load(path)
	require.NoError(t, err)

	// Check() must find 0 problems
	problems := r.Check()
	require.Empty(t, problems, "the fixture must have 0 problems")

	require.Len(t, r.Releases, 1)
	assert.Equal(t, "v1.1.0", r.Releases[0].Version)
	assert.Equal(t, 102, r.Releases[0].CardsCount)
	assert.Len(t, r.Releases[0].Streams, 25)

	// Round-trip byte stable assertion
	assert.Equal(t, string(original), string(r.Bytes()), "the fixture round-trip must be byte identical")
}

func TestTheRoadmapShapeSurvivesRemoveAndPull(t *testing.T) {
	path := filepath.Join("testdata", "synthetic-v1.1.0.sexp")
	original, err := os.ReadFile(path)
	require.NoError(t, err)

	dir := t.TempDir()
	tempPath := filepath.Join(dir, "nova-sprint.sexp")
	require.NoError(t, os.WriteFile(tempPath, original, 0644))

	r, err := Load(tempPath)
	require.NoError(t, err)

	// Remove v11-card-001
	removed, err := r.Remove("v11-card-001")
	require.NoError(t, err)
	assert.Equal(t, 1, removed)
	assert.Equal(t, 101, r.Releases[0].CardsCount)
	require.NoError(t, r.Save(tempPath))

	r2, err := Load(tempPath)
	require.NoError(t, err)
	assert.Empty(t, r2.Check())
	assert.Equal(t, 101, r2.Releases[0].CardsCount)

	// Pull v11-card-002
	outDir := filepath.Join(dir, "out")
	pulled, err := r2.Pull(outDir, "v11-card-002")
	require.NoError(t, err)
	assert.Len(t, pulled, 1)
	assert.Equal(t, 100, r2.Releases[0].CardsCount)

	pulledBrief, err := os.ReadFile(filepath.Join(outDir, "v11-card-002.md"))
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(pulledBrief), "REPO: example/repo\n"))

	require.NoError(t, r2.Save(tempPath))

	r3, err := Load(tempPath)
	require.NoError(t, err)
	assert.Empty(t, r3.Check())
	assert.Equal(t, 100, r3.Releases[0].CardsCount)
}

func TestAddRefusesAnIDAlreadyInTheRoadmapAndChangesNothing(t *testing.T) {
	dir := t.TempDir()
	briefDir := filepath.Join(dir, "incoming")
	require.NoError(t, os.MkdirAll(briefDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(briefDir, "card-0.md"), []byte("THE TASK. New.\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(briefDir, "card-3.md"), []byte("THE TASK. Again.\n"), 0644))

	r, err := LoadBytes([]byte(sampleRoadmap))
	require.NoError(t, err)
	_, err = r.Add("stream-a", briefDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `card "card-3" is already in the roadmap`)
	assert.Equal(t, sampleRoadmap, string(r.Bytes()), "a refused add leaves the roadmap as it was")
	assert.Equal(t, 3, r.Releases[0].CardsCount)
}

func TestPullRefusesAnEmptyBriefAndWritesNothing(t *testing.T) {
	dir := t.TempDir()
	empty := strings.Replace(sampleRoadmap, `"Brief 3 text"`, `""`, 1)
	r, err := LoadBytes([]byte(empty))
	require.NoError(t, err)

	outDir := filepath.Join(dir, "out")
	_, err = r.Pull(outDir, "card-1", "card-3")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `card "card-3" has an empty :brief`)
	assert.NoDirExists(t, outDir)
	assert.Equal(t, empty, string(r.Bytes()))
}

func TestCheckNamesACardWithNoID(t *testing.T) {
	noID := strings.Replace(sampleRoadmap, `:id "card-3" `, ``, 1)
	problems := CheckBytes([]byte(noID))
	require.Len(t, problems, 1)
	assert.Contains(t, problems[0], `a card with no :id in stream "stream-b"`)
}

func TestAddCutsALongTitleOnARuneBoundary(t *testing.T) {
	title, _, _ := parseBriefMetadata("THE TASK. "+strings.Repeat("a", 199)+"é and more", "x")
	assert.Equal(t, strings.Repeat("a", 199), title)
}

func TestCheckBytesNamesAnUnbalancedFileAsAProblem(t *testing.T) {
	problems := CheckBytes([]byte(sampleRoadmap[:len(sampleRoadmap)-3]))
	require.Len(t, problems, 1)
	assert.Empty(t, CheckBytes([]byte(sampleRoadmap)))
}
