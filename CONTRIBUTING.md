# Contributing

## First contribution

- **Share feedback.** Check the [open issues](https://github.com/GuiBibeau/artex-program/issues)
  for the same problem or proposal, and add new details there. For a new idea, explain what you
  tried to do and what would make it easier.
- **Report a problem.** Include a short reproduction and the environment. For a UI problem, a
  screenshot without sensitive data and the browser version help. Remove credentials, private URLs
  and sensitive task data before you post.
- **Report a security problem.** Do not open a public issue for a vulnerability. Follow
  [SECURITY.md](SECURITY.md) to report it in private.
- **Send a small change.** A correction of the documentation or a focused UI improvement is a good
  start. Check the [open pull requests](https://github.com/GuiBibeau/artex-program/pulls) for
  overlapping work. Refer to the relevant issue in your pull request, so that readers can find the
  discussion.

## Pull requests

- **At most 7 open pull requests per contributor.** Drafts count. When you reach the limit, close
  or merge one before you open another. Review is manual and handles one change at a time. A large
  batch slows the queue for everybody. If you have more changes, open one issue that lists them,
  and wait for the review of the current ones.
- **One focused change per pull request.** When a change has independent parts, split it into a
  small number of pull requests. State in each description what the other parts depend on.
- **Describe the change.** State the problem, the change in behavior, and what you tested. A pull
  request without a description is a request for changes.
- **Provider support stays generic.** Each OpenAI-compatible or Anthropic-compatible endpoint works
  through a model profile. A provider convenience goes in as a neutral preset (a name and a base
  URL) or as a recipe in the documentation. Do not add vendor logos, promotional codes, fixed
  vendor endpoints, or vendor-specific environment variables. A new wire format needs a schema
  migration, a profile save and load test, and a live connection test.

## Updating Docker image pins

The Dockerfile and the PostgreSQL service in `docker-compose.yml` use a readable tag followed by a
digest. Review these pins regularly for upstream security updates. When you refresh a pin:

1. Run `docker buildx imagetools inspect <image>:<tag>` and use the top-level index digest.
2. Check that the index includes the platforms that the image builds for. The base images are
   multi-arch. The Solana toolchain stage is `linux/amd64` only.
3. Record the old and the new digest in the pull request, so that reviewers can verify the change.

## Issues

- One problem per issue. Include the version or commit, what you ran, and what you expected.

## Security

- Do not open a public issue for a vulnerability in ARTEX Program. Follow the
  [instructions for a private report](SECURITY.md#reporting-a-vulnerability).
