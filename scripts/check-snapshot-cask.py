"""Check the expected generated text, without loading Ruby or installing a cask."""
import pathlib
import re
import sys


def check(text):
    if not re.search(r'^cask "fourbysix" do$', text, re.M):
        raise ValueError("missing fourbysix cask declaration")
    if "{{" in text or "}}" in text:
        raise ValueError("unresolved Go template")
    # Match the pinned generator's indentation; this is not Ruby semantic parsing.
    guard = r"^  on_macos do\n    depends_on macos: :ventura\n  end$"
    if len(re.findall(guard, text, re.M)) != 1:
        raise ValueError("expected one direct macOS Ventura guard")
    dependencies = re.findall(r"^\s*depends_on\b.*$", text, re.M)
    if dependencies != ["    depends_on macos: :ventura"]:
        raise ValueError("unexpected dependency could restrict Linux")
    # This intentionally checks the pinned generator's text shape, not Ruby
    # execution semantics. Each OS needs its own real two-architecture package
    # block; the separate guard alone cannot satisfy macOS package coverage.
    for platform, goos in (("macos", "darwin"), ("linux", "linux")):
        blocks = re.findall(r"^  on_" + platform + r" do\n(.*?)^  end$", text, re.M | re.S)
        packages = [block for block in blocks if "    on_intel do" in block or "    on_arm do" in block]
        if len(packages) != 1:
            raise ValueError("expected one package block: " + platform)
        block = packages[0]
        for ruby_arch, goarch in (("intel", "amd64"), ("arm", "arm64")):
            archive = r"fourbysix_[A-Za-z0-9.+_-]+_" + goos + "_" + goarch + r"\.tar\.gz"
            pattern = (
                r"^    on_" + ruby_arch + r" do\n"
                r'      sha256 "[0-9a-f]{64}"\n'
                r'      url "https://github\.com/zzwong/fourbysix/releases/download/[^/"\s]+/'
                + archive + r'"\n'
                r"    end$"
            )
            if len(re.findall(pattern, block, re.M)) != 1:
                raise ValueError("missing expected archive URL/hash: " + platform + "/" + goarch)
        if len(re.findall(r"^\s*url ", block, re.M)) != 2:
            raise ValueError("unexpected package URL count: " + platform)
    if not re.search(r'^  binary "fourbysix"$', text, re.M):
        raise ValueError("missing fourbysix binary stanza")


if __name__ == "__main__":
    source = pathlib.Path(sys.argv[1])
    if source.is_symlink() or not source.is_file():
        raise SystemExit("expected generated regular cask file")
    check(source.read_text(encoding="utf-8"))
    print("Generated cask text has scoped Ventura guard and Linux stanza.")
