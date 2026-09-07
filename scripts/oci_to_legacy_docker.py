#!/usr/bin/env python3
"""Convert buildx type=docker (OCI blobs) to docker-save v1 for RouterOS."""
import gzip, hashlib, io, json, sys, tarfile
from pathlib import Path

def main():
    src, dst = Path(sys.argv[1]), Path(sys.argv[2])
    with tarfile.open(src) as t:
        item = json.loads(t.extractfile("manifest.json").read())[0]
        cfg = t.extractfile(item["Config"]).read()
        layer = t.extractfile(item["Layers"][0]).read()
    if layer[:2] == b"\x1f\x8b":
        layer = gzip.decompress(layer)
    cfg_id = hashlib.sha256(cfg).hexdigest()
    layer_id = hashlib.sha256(layer).hexdigest()
    mf = [{"Config": cfg_id + ".json", "RepoTags": item.get("RepoTags") or ["mikrollm:arm64"],
           "Layers": [layer_id + "/layer.tar"]}]
    buf = io.BytesIO()
    with tarfile.open(fileobj=buf, mode="w") as out:
        def add(name, data):
            info = tarfile.TarInfo(name)
            info.size = len(data)
            info.mode = 0o644
            out.addfile(info, io.BytesIO(data))
        add("manifest.json", json.dumps(mf).encode())
        add(cfg_id + ".json", cfg)
        add(layer_id + "/layer.tar", layer)
    dst.write_bytes(buf.getvalue())
    print(dst, dst.stat().st_size)

if __name__ == "__main__":
    main()
