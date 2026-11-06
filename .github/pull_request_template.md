## Summary

<!-- What does this PR change and why? -->

Spec IDs: <!-- e.g. REQ-001, REQ-002 -->

## Testing

<!-- How was this verified? Commands run, tests added, manual checks. -->

## Security checklist (sensitive paths: migrations, auth, payments, device signatures, capacity)

- [ ] Authz + ownership checks
- [ ] Input validated
- [ ] Idempotency keys
- [ ] Lock order + no network I/O in transactions
- [ ] No secrets/PII in logs
- [ ] Migration is forward-only and safe
- [ ] Failure-path tests
- [ ] Automated risk review (`review-risk` / `/security-review`) linked
