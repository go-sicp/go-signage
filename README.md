# go-signage

An e-ink signage panel (IT8951 controller) driven over gRPC.

- `signage-agent` runs on the device (a Pi Zero 2W or any Linux box wired to
  the panel). It serves the Display gRPC API and can pull content from a
  `signage-content` server.
- `signage-cli` talks to an agent: `info`, `health`, `load-image`,
  `refresh`, `set-pull-mode`, `rotate`, `reissue`, `metrics`, `discover`,
  `connect`.

## Tokens

The agent generates a bearer token and an admin token on first start and
stores them in `<files-dir>/tls/token.txt` and `admin-token.txt` (mode 0600).

- The agent never prints a token. Its start-up banner names the token files
  and a `signage://host:port?fp=<fingerprint>` URL, which carries no token
  (v0.2.0; earlier versions printed both tokens and put the token in the URL).
- `signage-cli connect signage://...` refuses a URL that carries `token=`:
  a URL ends up in argv and shell history.
- Give the token to `signage-cli` by reference, never as a value:

  ```sh
  signage-cli -H panel.local --token-from systemd-creds:signage-token info
  signage-cli -H panel.local --token-from file:/run/secrets/signage-token info
  ```

  References are resolved with
  [go-authn/secretsource](https://github.com/go-authn/secretsource):
  `systemd-creds:NAME`, `keychain:SERVICE/ACCOUNT`, `keepassxc:ENTRY` or
  `file:PATH`. The plain `--token` / `-t` flag (v0.1.0) is deprecated, warns
  on stderr and is mutually exclusive with `--token-from`.
