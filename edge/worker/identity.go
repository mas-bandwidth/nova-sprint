package worker

// CommitEmailDomain is the domain of the git author email a bench's commits
// carry: <row>@CommitEmailDomain, with the row as the author name. Admission
// reads it to tell the human which identities a repository's contributor
// agreement has to be signed for.
const CommitEmailDomain = "nova-sprint.local"
