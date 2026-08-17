## Summary

<!-- Explain what this pull request changes and why. Focus on the user-facing problem and behavior. -->

## Related issue

<!-- Link the relevant issue, for example: Closes #123. Use "N/A" when no issue is required. -->

Closes #

## Type of change

<!-- Select all that apply. -->

- [ ] Bug fix
- [ ] New feature
- [ ] Documentation
- [ ] Tests
- [ ] Refactoring or maintenance
- [ ] CLI change
- [ ] Breaking change

## Behavior and compatibility

<!--
Describe observable behavior, exported API changes, error semantics, GraphQL or
HTTP contract assumptions, and any migration steps. Write "No compatibility
impact" when applicable.
-->

### Infrahub compatibility

<!-- State the Infrahub server or Python SDK version used to verify version-sensitive behavior, or "N/A". -->

## Testing

<!-- List the commands and relevant scenarios used to verify this change. -->

```text
make check
make race
```

## Checklist

- [ ] The change is focused and preserves unrelated work.
- [ ] New or changed behavior has meaningful tests.
- [ ] Exported identifiers have appropriate Go documentation.
- [ ] User-facing documentation and examples are updated, or no update is needed.
- [ ] `docs/compatibility.md` is updated for Python SDK capability changes, or no update is needed.
- [ ] `make check` passes.
- [ ] `make race` passes.
- [ ] `make build` passes when command code changed, or is not applicable.
- [ ] No credentials, customer schemas, production endpoints, or sensitive response data are included.
- [ ] Any skipped check, unverified assumption, or compatibility concern is explained below.

## Reviewer notes

<!-- Add anything reviewers should examine closely, including skipped checks or follow-up work. -->
