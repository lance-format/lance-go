# Task-only cloud acceptance transport

These operator scripts belong only to this task branch, not the SDK PR.

`credential_bridge.py serve` runs on Adam. Its first request obtains A from
the supplied issuer argv, its second obtains B, and later requests reuse B.
Only request numbers, credential fingerprints and expiration times enter the
server audit log. The server stops after a bounded lifetime. The issuer uses
the existing local SSO helper to assume an existing role with the task's
restrictive inline session policy. SSO credentials never leave Adam.

Both socket directories must already exist, belong to the invoking user and
have mode 0700. The local socket is 0600. Forward it using existing SSH access
with `-N -o ExitOnForwardFailure=yes -R REMOTE_SOCKET:LOCAL_SOCKET`. Do not
expose a TCP listener or enable remote external binding. Verify remote socket
permissions before invoking the client.

`credential_bridge.py fetch --socket REMOTE_SOCKET` is the runner's
`credential_process` command. **Its stdout contains secret values.** Connect
it directly to the runner; never run it in a logged terminal or save its output.
Do not probe the actual credential socket before the run: a probe consumes A.
Test transport using a separate socket and nonsecret fixture issuer first.

Source arrives at NieR only by Git, fetched at the exact operator commit. It
may be extracted to the evidence directory from that commit after the worker
owning the registered checkout has restored it. No credentials, SSO caches,
policy/account configuration, or run data are committed.

Mint actual credentials only when the runner is built and ready. Use a fresh
private socket directory and fresh broker for each attempt. The broker caches
only short-lived scoped STS credentials in memory. Stop the SSH forward and
broker after the run; retain only safe audit output. Cleanup is restricted to
the exact unique `run_uri` emitted by the runner, never the caller's prefix.
