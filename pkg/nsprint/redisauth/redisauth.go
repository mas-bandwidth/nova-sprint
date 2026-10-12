// Package redisauth is the one fleet Redis seat every tool dials with from
// the environment: the ACL user and the variable holding its password.
// It is a leaf (os and fmt only) so a tool that must import no network or
// subprocess code, nova-tokens under its boundary tests, reads its login here
// rather than through internal/nsprint/store, which reaches net and, through
// pkg/seatcred, os/exec.
package redisauth

// UserEnv names the ACL user and turns authentication on; PasswordEnvEnv names
// the variable holding that user's password, DefaultPasswordEnv when unset.
const (
	UserEnv            = "NOVA_SPRINT_REDIS_USER"
	PasswordEnvEnv     = "NOVA_SPRINT_REDIS_PASSWORD_ENV"
	DefaultPasswordEnv = "NOVA_REDIS_BENCH_PASSWORD"
)
