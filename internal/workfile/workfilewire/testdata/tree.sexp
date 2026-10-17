; A work file tree in the hand-editable text form (the Tree table of
; schemas/workfile/workfile.schema): one scoped import of two repositories.
(:format "v1" :source "github" :org "acme"
 :fetched "2026-10-15T08:00:00Z" ; when the fetch began, UTC
 :scope ((:text "acme/a") (:text "acme/b"))

 ; every repository, sorted by name
 :repos
 ((:name "acme/a" :url "https://github.com/acme/a" :archived false
   :issues
   ((:number 7 :url "https://github.com/acme/a/issues/7" :node_id "I_kwDO7"
     :title "Crash on start" :state "OPEN" :state_reason "" :origin "internal"
     :author "alice" :author_association "MEMBER"
     :created "2026-10-01T10:00:00Z" :updated "2026-10-02T11:00:00Z" :closed ""
     :locked false :lock_reason ""
     :labels ((:text "bug") (:text "p1")) :assignees ((:text "bob"))
     :milestone (:number 3 :title "v1.5")
     :body "It crashes.\nEvery time; \"always\"."
     :comments
     ((:id "IC_kwDO1" :url "https://github.com/acme/a/issues/7#issuecomment-1"
       :author "bob" :author_association "COLLABORATOR"
       :created "2026-10-01T10:05:00Z" :updated "2026-10-01T10:06:00Z"
       :body "Seen it."))
     :references
     ((:kind "PullRequest" :repo "acme/b" :number 12 :url "https://github.com/acme/b/pull/12"
       :actor "carol" :at "2026-10-02T09:00:00Z" :will_close true)
      ; a reference whose source this login does not see: no kind, number 0
      (:kind "" :repo "" :number 0 :url "" :actor "dave" :at "2026-10-02T09:30:00Z" :will_close false))
     :linked_prs
     ((:repo "acme/b" :number 12 :url "https://github.com/acme/b/pull/12" :state "OPEN")))

    ; the largest number the tree carries, 1<<31
    (:number 2147483648 :url "https://github.com/acme/a/issues/2147483648" :node_id "I_kwDO8"
     :title "Old" :state "CLOSED" :state_reason "NOT_PLANNED" :origin "external"
     :author "erin" :author_association "NONE"
     :created "2026-09-01T10:00:00Z" :updated "2026-09-02T10:00:00Z" :closed "2026-09-03T10:00:00Z"
     :locked true :lock_reason "RESOLVED"
     :labels () :assignees () :milestone null :body ""
     :comments () :references () :linked_prs ())))
  (:name "acme/b" :url "https://github.com/acme/b" :archived true :issues ())))
; end of the tree
