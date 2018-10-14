cf
==

cf is a command line tool that allows you to view and manipulate DNS records
stored in your [Cloudflare](https://www.cloudflare.com) account.

## Interactive mode

If you launch the `cf` tool without command line arguments, it will run in
interactive mode. In interactive mode, you will be shown a `cf>` prompt, where
you can type in commands. For example, type `help`:

```text
cf> help
Primary commands:
    list    List all DNS records
    ip4     Add or modify an IPv4 Address (type A) record
    ip6     Add or modify an IPv6 Address (type AAAA) record
    cname   Add or modify a CNAME record
    txt     Add or modify a text (type TXT) record
    add     Add a DNS record
    delete  Delete DNS record(s)
    zone    Set active zone
    quit    Quit the application

```

To obtain further help on a specific command, type `help <cmd>`.  For
example:

```text
cf> help ip4
Usage: ip4 <name> <address>
Description:
   Add or modify an IPv4 address (type A) DNS record in the currently active
   zone.

Shortcut: ip
```

The following example updates the address record for `foo.example.com` so that
it points to `10.0.0.1`:

```text
cf> ip4 foo.example.com 10.0.0.1
DNS record updated.
```

Some commands require Cloudflare credentials, which you will be prompted for
when you issue the command.  All future commands you enter during the same
interactive session will rely on these credentials, so you only need to enter
them once. If you prefer to provide the credentials through environment
variables, that is also possible.  See the next section for details.

## Non-interactive mode

You can also use the tool in non-interactive mode by passing all command
requests directly on the command line. For example, type `cf help` from
the shell:

```text
$ cf help
Primary commands:
    list    List all DNS records
    ip4     Add or modify an IPv4 Address (type A) record
    ip6     Add or modify an IPv6 Address (type AAAA) record
    cname   Add or modify a CNAME record
    txt     Add or modify a text (type TXT) record
    add     Add a DNS record
    delete  Delete DNS record(s)
    zone    Set active zone
    quit    Quit the application
```

Since cloudflare credentials cannot be requested in non-interactive mode, you
will need to provide them through the following environment variables:

| Variable         | Description                           |
|------------------|---------------------------------------|
| CLOUDFLARE_EMAIL | Your cloudflare account email address |
| CLOUDFLARE_KEY   | Your cloudflare API key               |
| CLOUDFLARE_ZONE  | Your cloudflare zone name             |


On Mac and Linux, this can be done in the bash shell as in the following
example:

```text
$ CLOUDFLARE_EMAIL=me@email.com \
CLOUDFLARE_KEY=d299c6cdc6464f35a0f45fc789eb12a2 \
CLOUDFLARE_ZONE=example.com \
cf list
```

On Windows, you can do this:

```text
C:\>set CLOUDFLARE_EMAIL=me@email.com
C:\>set CLOUDFLARE_KEY=d299c6cdc6464f35a0f45fc789eb12a2
C:\>set CLOUDFLARE_ZONE=example.com
C:\>cf list
```