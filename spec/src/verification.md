# Verification

| Requirement | Evidence                                                                   |
| ----------- | -------------------------------------------------------------------------- |
| NA-ARCH-001 | vendor manifest/pin check, Go tests, Docker build, full-SHA rejection test |
| NA-ARCH-002 | rendered development and production Compose assertions                     |
| NA-SEC-001  | registration request-shape tests and live onboarding smoke                 |
| NA-SEC-002  | log-filter tests and post-smoke canary scan                                |
| NA-API-001  | client unit tests and live NIP-46 signing smoke                            |
| NA-OPS-001  | volume assertions and backup manifest tests                                |
| NA-TEST-001 | Docker smoke plus protocol recovery tests                                  |

Real Google OAuth is a manual Coolify staging gate because upstream requires interactive provider
login and explicit recovery confirmation. Automated tests use a valid central-signed token and
production authentication code paths; no authentication bypass is added.
