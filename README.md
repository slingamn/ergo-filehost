ergo-filehost
=============

This is an alpha implementation of [draft/filehost](https://github.com/ircv3/ircv3-specifications/pull/562) for use with the [Ergo IRC server](https://github.com/ergochat/ergo), built using Claude Code. It lacks proper resource and rate limiting, as well as other safeguards against abuse, so it is only recommended for use in private deployments of Ergo that are only accessible to trusted users.

See `config.yaml` for an example config of this service. On the Ergo side, you will need to configure the `api` block and also add the correct upload endpoint to `server.additional-isupport`:

```yaml
    # publish additional key-value pairs in ISUPPORT (the 005 numeric).
    additional-isupport:
        "draft/FILEHOST": "https://example.com/filehost/upload"
        "soju.im/FILEHOST": "https://example.com/filehost/upload"
```
