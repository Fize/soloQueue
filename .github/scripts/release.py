"""Small, stdlib-only helpers for the tag release workflow."""

import base64
import hashlib
import json
import os
import re
import subprocess
import sys
import urllib.error
import urllib.parse
import urllib.request


def metadata(tag, repository):
    # Build metadata (+...) is excluded because '+' is not a Docker tag character.
    number = r"(?:0|[1-9][0-9]*)"
    identifier = r"(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)"
    if not re.fullmatch(rf"v{number}\.{number}\.{number}(?:-{identifier}(?:\.{identifier})*)?", tag):
        raise ValueError("Expected vMAJOR.MINOR.PATCH[-PRERELEASE], without build metadata")
    if len(tag) > 128:
        raise ValueError("Version exceeds Docker's 128-character tag limit")
    return {"version": tag[1:], "prerelease": str("-" in tag).lower(),
            "image": "ghcr.io/" + repository.lower()}


def fingerprint():
    # Exact tracked trees copied by Dockerfile, plus the recipe and CI tooling.
    # Root docs, deploy docs, and skills are not Docker build inputs.
    paths = ["cmd", "internal", "web", "status-ui", "go.mod", "go.sum",
             "deploy/docker/Dockerfile", ".dockerignore", ".github"]
    tree = subprocess.check_output(["git", "ls-tree", "-r", "-z", "HEAD", "--", *paths])
    return hashlib.sha256(tree).hexdigest()


def manifest_digest(image, tag, actor, password, opener=urllib.request.urlopen):
    repository = image.removeprefix("ghcr.io/")
    # Push scope permits GHCR to authorize creation of a not-yet-existing package.
    # A pull-only token can be denied before we can observe its manifest 404.
    query = urllib.parse.urlencode({"service": "ghcr.io", "scope": f"repository:{repository}:pull,push"})
    credentials = base64.b64encode(f"{actor}:{password}".encode()).decode()
    request = urllib.request.Request("https://ghcr.io/token?" + query,
                                     headers={"Authorization": "Basic " + credentials})
    # Authentication and network errors must fail, never masquerade as a miss.
    with opener(request, timeout=30) as response:
        token = json.load(response)["token"]
    request = urllib.request.Request(f"https://ghcr.io/v2/{repository}/manifests/{tag}",
        method="HEAD", headers={"Authorization": "Bearer " + token,
        "Accept": ", ".join(["application/vnd.oci.image.index.v1+json",
            "application/vnd.oci.image.manifest.v1+json",
            "application/vnd.docker.distribution.manifest.list.v2+json",
            "application/vnd.docker.distribution.manifest.v2+json"])})
    try:
        with opener(request, timeout=30) as response:
            digest = response.headers.get("Docker-Content-Digest", "")
    except urllib.error.HTTPError as error:
        if error.code == 404:
            return ""
        raise
    if not re.fullmatch(r"sha256:[0-9a-f]{64}", digest):
        raise ValueError("Registry returned no valid manifest digest")
    return digest


def output(values):
    with open(os.environ["GITHUB_OUTPUT"], "a", encoding="utf-8") as stream:
        for key, value in values.items():
            stream.write(f"{key}={value}\n")


if __name__ == "__main__":
    if sys.argv[1] == "metadata":
        output(metadata(os.environ["GITHUB_REF_NAME"], os.environ["GITHUB_REPOSITORY"]))
    elif sys.argv[1] == "image":
        cache_tag = "build-" + fingerprint()
        digest = manifest_digest(os.environ["IMAGE"], cache_tag,
                                 os.environ["GITHUB_ACTOR"], os.environ["GH_TOKEN"])
        output({"cache_tag": cache_tag, "digest": digest,
                "build": str(not digest).lower()})
    else:
        raise ValueError("Unknown release command")
