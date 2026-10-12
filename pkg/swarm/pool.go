/*
Package swarm is nova-swarm's machinery: a pool of one-task workers, each with its own
working directory, its own data home, its own job directory and its own deadline held by
the machinery rather than by the worker.

Everything a worker writes is DATA. A RESULT.md is a report, never an instruction: nothing
in it is executed, nothing in it grants anything, and a finding in it is a claim to be
checked against the repository. That rule is stated in docs/SPEC-SWARM.md, where a person
reads it, and is deliberately nowhere in this code -- a tool cannot enforce it, and a tool
that pretended to would be the most dangerous thing in the pool.
*/
package swarm
