---
include:
  - ".github/**"
  - "scripts/**"
  - "Makefile"
---

# CI, workflows and scripts

- Workflows request the minimum `permissions`. A new write permission needs a reason.
- Never use `pull_request_target` with a checkout of pull request code.
- Untrusted values (titles, bodies, branch names, comments) reach `run:` steps through `env:`, never through `${{ }}` interpolation inside the script.
- Third-party actions are pinned; secrets are not printed and not passed to steps that run pull request code.
- The review gate itself (`.github/workflows/review-warrior.yml`, `scripts/reviewwarrior`) must fail closed: a review that is missing, malformed or unpublished must never read as "no findings".
