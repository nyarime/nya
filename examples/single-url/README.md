# Single-URL distribution (embedded download index)

Sender publishes **one** `.nya` link. Receiver bootstraps transport blocks from
the embedded index — no sidecar `.nyam` required.

## Sender

```bash
nya create -level 9 -solid -fec 15 pack.nya ./GameData/   # embeds download index by default
# upload pack.nya to CDN / Cloudflare Tunnel / object storage
# opt out at create: nya create -no-embed …
# later: nya manifest add pack.nya   /   nya manifest del pack.nya
```

## Receiver

```bash
nya-get --url https://cdn.example.com/pack.nya
nya verify pack.nya
nya open pack.nya          # or: nya extract pack.nya ./out
```

## How it works

1. `nya-get` reads remote size, then the last 40 bytes (`NYADIDX1` footer).
2. Fetches the download-index tail and builds an in-memory manifest.
3. Parallel `Range` downloads of body blocks; verifies body BLAKE3.
4. Trailing index is not required for `nya extract` / `Open`.

Sidecar `.nyam` remains optional for CDN workflows that prefer a separate JSON
index (`nya manifest -o pack.nyam --url …`).
