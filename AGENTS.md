# AGENTS.md

Rules for every worker, AI or person, in this repository. CONTRIBUTING.md covers how review goes.

- **Before writing new code, check it already exists.** Search nova-sprint and nova-tools for the mechanism by name and by behaviour, read `git log --since=7.days` and the latest handoff, and read the help the installed tools print. If it exists, extend it. Two paths for one thing is a defect, consolidated before release.
- **Reach a friend only through nova-hook push notifications.** It is forbidden to wake a friend or communicate with one any other way: a message goes on the bus and nova-hook pushes it into her session. No typed keystrokes, window or terminal automation, wake files, ad-hoc watchers or daemon deliveries. A harness that needs delivery gets it by extending nova-hook.
