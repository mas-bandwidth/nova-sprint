// The issues group of nova-sprint: nova-sprint issues import, issues verify and
// issues roadmap are the folded-in work verbs, gathered into nova-sprint (one
// tool). Each delegates to the issues sub-tool (internal/issues), so every flag,
// output line, exit code, refusal and GitHub access is unchanged. Their -h is
// the app's own help (the concrete synopsis from the verb table, not the
// sub-tool's [flags] placeholder), so the group meets nova-sprint's help like
// every other verb.
//
// The per-verb dispatch lives in issues_import.go, issues_verify.go and
// issues_roadmap.go.
package main
