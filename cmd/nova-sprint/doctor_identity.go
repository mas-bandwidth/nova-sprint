package main

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/oneline"
)

func (a *app) doctorMachineGitIdentity(ctx context.Context) sprint.DoctorCheck {
	const check, remedy = "machine-git-identity", "set NOVA_SPRINT_GIT_NAME and NOVA_SPRINT_GIT_EMAIL to the configured machine identity"
	id, err := a.configuredMachineIdentity()
	if err != nil {
		return sprint.DoctorCheck{Area: sprint.AreaSprint, Check: check, Subject: "machine", Status: sprint.DoctorFail,
			Evidence: oneline.Escape(err.Error()), Remedy: remedy}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "config", "--global", "--get", "user.email")
	out, err := cmd.Output()
	if err != nil {
		if e, ok := err.(*exec.ExitError); !ok || e.ExitCode() != 1 {
			return sprint.DoctorCheck{Area: sprint.AreaSprint, Check: check, Subject: "machine", Status: sprint.DoctorWarn,
				Evidence: "configured=" + oneline.Escape(id.Name+" <"+id.Email+">") + "; host global user.email could not be read: " + oneline.Escape(err.Error()), Remedy: "check git and the host's global Git configuration"}
		}
		return sprint.DoctorCheck{Area: sprint.AreaSprint, Check: check, Subject: "machine", Status: sprint.DoctorOK,
			Evidence: "configured=" + oneline.Escape(id.Name+" <"+id.Email+">") + "; host global user.email is unset", Remedy: "-"}
	}
	globalEmail := strings.TrimSpace(string(out))
	if globalEmail == id.Email {
		return sprint.DoctorCheck{Area: sprint.AreaSprint, Check: check, Subject: "machine", Status: sprint.DoctorFail,
			Evidence: "configured=" + oneline.Escape(id.Name+" <"+id.Email+">") + "; host global user.email equals the machine email", Remedy: "set NOVA_SPRINT_GIT_EMAIL to a machine-specific email distinct from the host global user.email"}
	}
	return sprint.DoctorCheck{Area: sprint.AreaSprint, Check: check, Subject: "machine", Status: sprint.DoctorOK,
		Evidence: "configured=" + oneline.Escape(id.Name+" <"+id.Email+">") + "; host global user.email=" + oneline.Escape(globalEmail), Remedy: "-"}
}

