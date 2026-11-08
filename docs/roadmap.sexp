; docs/roadmap.sexp: the data the tables in ROADMAP.md are generated from (nova-roadmap render).
(:schema 1
 :root "goal"
 :page (:roadmaps ("future"))
 :nodes
 ((:id "goal" :type :work-set :children ("future"))
  (:id "future" :type :roadmap :title "Future work"
   :rows (
     ("admission-refuses-functional-done-when" "add refuses a DONE-WHEN that runs functional tests, which cannot run inside the wall")
     ("deal-skips-old-binary-rows" "The dealer never deals to a row whose advertised binary is older than the run's version")
     ("missing-toolchain-refuses" "A missing toolchain inside the wall is a refusal at admission or a fault, never a bounce")
     ("per-row-stream-exclusion" "Exclude a stream from a row (needs a schema change)")
     ("runner-registrations-as-records" "Runner registrations stored as nova-config records")
     ("ci-macos-functional-leg" "ci-macos-functional-leg — PR #710, branch ci/macos-functional-govulncheck: macOS functional CI job, missing prerequisite programs fail the job, govulncheck")
     ("ci-staticcheck-deadcode-and-guides" "ci-staticcheck-deadcode-and-guides — PR #712, branch hygiene/staticcheck-deadcode-readme: staticcheck and deadcode in CI, the guides' cold commands run against the binary")
     ("steps-model-covers-holds-and-limits" "Steps.tla gains the holds/limits/silence/Gone step (the model gap under #702's bug)")
     ("policy-limits-read-by-steps" "The nine validated policy limits read by the steps instead of Engine.* constants")
     )
   :columns (("done" "done"))
   :cells (
     ("admission-refuses-functional-done-when" "done" "t-admission-refuses-functional-done-when")
     ("deal-skips-old-binary-rows" "done" "t-deal-skips-old-binary-rows")
     ("missing-toolchain-refuses" "done" "t-missing-toolchain-refuses")
     ("per-row-stream-exclusion" "done" "t-per-row-stream-exclusion")
     ("runner-registrations-as-records" "done" "t-runner-registrations-as-records")
     ("ci-macos-functional-leg" "done" "t-ci-macos-functional-leg")
     ("ci-staticcheck-deadcode-and-guides" "done" "t-ci-staticcheck-deadcode-and-guides")
     ("steps-model-covers-holds-and-limits" "done" "t-steps-model-covers-holds-and-limits")
     ("policy-limits-read-by-steps" "done" "t-policy-limits-read-by-steps")
     ))
  (:id "t-admission-refuses-functional-done-when" :type :task :title "add refuses a DONE-WHEN that runs functional tests, which cannot run inside the wall" :state :todo)
  (:id "t-deal-skips-old-binary-rows" :type :task :title "The dealer never deals to a row whose advertised binary is older than the run's version" :state :todo)
  (:id "t-missing-toolchain-refuses" :type :task :title "A missing toolchain inside the wall is a refusal at admission or a fault, never a bounce" :state :todo)
  (:id "t-per-row-stream-exclusion" :type :task :title "Exclude a stream from a row (needs a schema change)" :state :todo)
  (:id "t-runner-registrations-as-records" :type :task :title "Runner registrations stored as nova-config records" :state :todo)
  (:id "t-ci-macos-functional-leg" :type :task :title "ci-macos-functional-leg — PR #710, branch ci/macos-functional-govulncheck: macOS functional CI job, missing prerequisite programs fail the job, govulncheck" :state :todo)
  (:id "t-ci-staticcheck-deadcode-and-guides" :type :task :title "ci-staticcheck-deadcode-and-guides — PR #712, branch hygiene/staticcheck-deadcode-readme: staticcheck and deadcode in CI, the guides' cold commands run against the binary" :state :todo)
  (:id "t-steps-model-covers-holds-and-limits" :type :task :title "Steps.tla gains the holds/limits/silence/Gone step (the model gap under #702's bug)" :state :todo)
  (:id "t-policy-limits-read-by-steps" :type :task :title "The nine validated policy limits read by the steps instead of Engine.* constants" :state :todo :note "escalate_after frontier_after flap_limit idle_floor land_target faults same_finding reports_per_tick route_rest_window")))
