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
     ("steps-model-covers-holds-and-limits" "Steps.tla gains the holds/limits/silence/Gone step (the model gap under #702's bug)")
     ("policy-limits-read-by-steps" "The nine validated policy limits read by the steps instead of Engine.* constants")
     ("policy-diff" "policy diff vA vB: a line diff of two policy versions (deleted 2026-11-08; policy show each and diff them)")
     )
   :columns (("done" "done"))
   :cells (
     ("admission-refuses-functional-done-when" "done" "t-admission-refuses-functional-done-when")
     ("deal-skips-old-binary-rows" "done" "t-deal-skips-old-binary-rows")
     ("missing-toolchain-refuses" "done" "t-missing-toolchain-refuses")
     ("per-row-stream-exclusion" "done" "t-per-row-stream-exclusion")
     ("runner-registrations-as-records" "done" "t-runner-registrations-as-records")
     ("steps-model-covers-holds-and-limits" "done" "t-steps-model-covers-holds-and-limits")
     ("policy-limits-read-by-steps" "done" "t-policy-limits-read-by-steps")
     ("policy-diff" "done" "t-policy-diff")
     ))
  (:id "t-admission-refuses-functional-done-when" :type :task :title "add refuses a DONE-WHEN that runs functional tests, which cannot run inside the wall" :state :todo)
  (:id "t-deal-skips-old-binary-rows" :type :task :title "The dealer never deals to a row whose advertised binary is older than the run's version" :state :todo)
  (:id "t-missing-toolchain-refuses" :type :task :title "A missing toolchain inside the wall is a refusal at admission or a fault, never a bounce" :state :todo)
  (:id "t-per-row-stream-exclusion" :type :task :title "Exclude a stream from a row (needs a schema change)" :state :todo)
  (:id "t-runner-registrations-as-records" :type :task :title "Runner registrations stored as nova-config records" :state :todo)
  (:id "t-steps-model-covers-holds-and-limits" :type :task :title "Steps.tla gains the holds/limits/silence/Gone step (the model gap under #702's bug)" :state :todo)
  (:id "t-policy-limits-read-by-steps" :type :task :title "The nine validated policy limits read by the steps instead of Engine.* constants" :state :todo :note "escalate_after frontier_after flap_limit idle_floor land_target faults same_finding reports_per_tick route_rest_window")
  (:id "t-policy-diff" :type :task :title "policy diff vA vB: a line diff of two policy versions (deleted 2026-11-08; policy show each and diff them)" :state :todo :note "was modules/policy/diff.go, a linear-space LCS refused above 10,000 lines")))
