#!/usr/bin/env python3
"""Convert buildx type=docker (OCI blobs) to docker-save v1 for RouterOS.

RouterOS wants a docker-save v1 tar (manifest.json + Config + layer.tar).
Images with more than one layer (e.g. CA certs + binary) are squashed into a
single uncompressed layer so ROS still sees a valid v1 layout.
"""
import gzip
import hashlib
import io
import json
import sys
import tarfile
import tempfile
from pathlib import Path


def read_blob(t: tarfile.TarFile, name: str) -> bytes:
    data = t.extractfile(name).read()
    if data[:2] == b"\x1f\x8b":
        data = gzip.decompress(data)
    return data


def squash_layers(blobs: list[bytes]) -> bytes:
    extract_kw = {}
    if hasattr(tarfile, "data_filter"):
        extract_kw["filter"] = "fully_trusted"
    with tempfile.TemporaryDirectory() as td:
        root = Path(td) / "root"
        root.mkdir()
        for blob in blobs:
            with tarfile.open(fileobj=io.BytesIO(blob), mode="r:") as lt:
                lt.extractall(root, **extract_kw)
        out = io.BytesIO()
        with tarfile.open(fileobj=out, mode="w") as ot:
            ot.add(root, arcname=".")
        return out.getvalue()


def add(out: tarfile.TarFile, name: str, data: bytes) -> None:
    info = tarfile.TarInfo(name)
    info.size = len(data)
    info.mode = 0o644
    out.addfile(info, io.BytesIO(data))


def main() -> None:
    src, dst = Path(sys.argv[1]), Path(sys.argv[2])
    with tarfile.open(src) as t:
        item = json.loads(t.extractfile("manifest.json").read())[0]
        cfg = json.loads(t.extractfile(item["Config"]).read())
        layers = [read_blob(t, name) for name in item["Layers"]]
    layer = squash_layers(layers) if len(layers) > 1 else layers[0]
    diff = "sha256:" + hashlib.sha256(layer).hexdigest()
    cfg["rootfs"] = {"type": "layers", "diff_ids": [diff]}
    cfg_raw = json.dumps(cfg, separators=(",", ":")).encode()
    cfg_id = hashlib.sha256(cfg_raw).hexdigest()
    layer_id = hashlib.sha256(layer).hexdigest()
    mf = [{
        "Config": cfg_id + ".json",
        "RepoTags": item.get("RepoTags") or ["mikrollm:arm64"],
        "Layers": [layer_id + "/layer.tar"],
    }]
    buf = io.BytesIO()
    with tarfile.open(fileobj=buf, mode="w") as out:
        add(out, "manifest.json", json.dumps(mf).encode())
        add(out, cfg_id + ".json", cfg_raw)
        add(out, layer_id + "/layer.tar", layer)
    dst.write_bytes(buf.getvalue())
    print(dst, dst.stat().st_size)


if __name__ == "__main__":
    main()
