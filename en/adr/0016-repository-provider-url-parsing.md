# ADR 0016: Repository provider URL parsing

## Status

Accepted.

## Decision

Repository provider adapters must parse user-supplied repository URLs into canonical identity fields:

```text
provider
provider_host
full_path
protocol
clone_url
```

Canonical repository identity remains `provider + provider_host + full_path` from ADR 0013. URL parsing must produce identical identity fields for equivalent HTTPS, SSH URL and SSH scp-like inputs.

## Common rules

Supported input shapes:

```text
https://host/group/repo.git
ssh://git@host/group/repo.git
git@host:group/repo.git
host/group/repo
```

Normalization rules:

- `provider_host` is lower-case;
- `provider_host` includes a non-default port when required to distinguish provider instances;
- protocol, username, password, token, query and fragment are removed from identity fields;
- `full_path` has no leading slash and no trailing `.git`;
- path separator is `/`;
- empty path segments are rejected;
- `.` and `..` path segments are rejected;
- backslash path separators are rejected;
- control characters are rejected;
- URL userinfo is rejected for persistence and must not appear in `clone_url`;
- safe-normalized `clone_url` is optional transport metadata and not identity.

Machine-readable validation error codes:

```text
provider_host_required
unsupported_provider_url
unsupported_url_protocol
invalid_repository_path
invalid_provider_host
credential_userinfo_not_allowed
provider_path_shape_mismatch
```

## Provider-specific rules

### GitLab

GitLab repository `full_path` supports nested groups:

```text
group/subgroup/project
```

Accepted examples:

```text
https://gitlab.example.com/group/repo.git
https://gitlab.example.com/group/subgroup/repo
ssh://git@gitlab.example.com/group/subgroup/repo.git
git@gitlab.example.com:group/subgroup/repo.git
```

Canonical result:

```text
provider = gitlab
provider_host = gitlab.example.com
full_path = group/subgroup/repo
```

GitLab recursive group clone uses `group_path`, normalized with the same path rules, but `group_path` does not require a repository leaf and must not end with `.git`.

### GitHub

GitHub and GitHub Enterprise Server repository `full_path` must have exactly two path segments:

```text
owner/repo
```

Accepted examples:

```text
https://github.com/org/repo.git
ssh://git@github.com/org/repo.git
git@github.com:org/repo.git
https://ghe.company.internal/org/repo
```

Canonical result:

```text
provider = github
provider_host = github.com or ghe.company.internal
full_path = org/repo
```

More than two path segments are rejected with `provider_path_shape_mismatch`.

### Bitbucket

Bitbucket Cloud repository `full_path` must have exactly two path segments:

```text
workspace/repo_slug
```

Bitbucket Data Center accepts these input shapes and normalizes all of them to `PROJECT_KEY/repo_slug`:

```text
https://bitbucket.company.local/scm/PROJ/repo.git
ssh://git@bitbucket.company.local/PROJ/repo.git
ssh://git@bitbucket.company.local:7999/PROJ/repo.git
https://bitbucket.company.local/projects/PROJ/repos/repo
```

Data Center project keys are normalized to uppercase. Repository slug casing is preserved unless the provider adapter has provider-specific evidence that lower-casing is required.

### Azure DevOps

Azure DevOps canonical identity treats organization as part of `provider_host` and project/repository as `full_path`:

```text
provider_host = dev.azure.com/{organization}
full_path = {project}/{repo}
```

Accepted examples:

```text
https://dev.azure.com/org/project/_git/repo
https://org@dev.azure.com/org/project/_git/repo
https://ssh.dev.azure.com/v3/org/project/repo
git@ssh.dev.azure.com:v3/org/project/repo
```

Canonical result:

```text
provider = azure_devops
provider_host = dev.azure.com/org
full_path = project/repo
```

`ssh.dev.azure.com` input is normalized to the same provider host form `dev.azure.com/{organization}`.

## Rationale

Repository identity depends on stable parsing. Without explicit provider rules, equivalent clone URLs can create duplicate repository cards or route operations to the wrong provider host.

GitLab, GitHub, Bitbucket and Azure DevOps use different path shapes. Provider adapters need strict, testable parsing behavior before their implementation stage. Stage 05 MVP implements only `generic` Git plus one managed provider from `gitlab`/`github`; the remaining provider rules are still canonical for Stage 05A/platform extensions.

## Consequences

- Stage 05 provider adapters must implement the parsing rules for the providers included in Stage 05 MVP.
- Stage 05A/platform provider adapters must implement these rules before enabling `bitbucket`, `azure_devops` or the second managed provider from `gitlab`/`github`.
- Stage 05 no longer has a blocker for provider-specific URL parsing edge cases for its MVP provider set.
- API validation must return machine-readable error codes for unsupported or ambiguous URLs.
- Test fixtures must include positive and negative URL parsing cases for each provider.
- `clone_url` remains safe-normalized transport metadata and must not include userinfo or secrets.
