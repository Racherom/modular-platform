# Security

## Reporting a flaw

Please do not open a public issue for a security flaw. Report it privately through GitHub's
private vulnerability reporting: on https://github.com/racherom/modular-platform, open the
Security tab and choose "Report a vulnerability".

Include what you found, how to reproduce it, and which version or commit you tested. You will get
an answer within a week. Once a fix is released, the advisory is published with credit to you,
unless you ask otherwise.

## Scope

In scope: the renderer, web and other platform images, the rendered zips (`docker-compose.yml`,
`setup.sh`), and the online editor's Compose files in `deploy/`.

Third party images that catalog modules wrap (for example `postgres`) are out of scope; report
those to their maintainers.
