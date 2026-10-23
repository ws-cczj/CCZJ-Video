"""Independently derive the public webfont cmap by exact Adobe glyph outlines.

Requires requests and fonttools. Does not run downloaded code. Does not read or
copy a third-party decoder table. No font binary is retained in the repository.
"""
import hashlib
import io
import json
from pathlib import Path

import requests
from fontTools.pens.recordingPen import RecordingPen
from fontTools.ttLib import TTFont

OUT = Path(__file__).parent
SOURCE = "https://raw.githubusercontent.com/adobe-fonts/source-han-sans/2.002R/OTF/SimplifiedChinese/SourceHanSansSC-Normal.otf"
SUBSET = "https://lf6-awef.bytetos.com/obj/awesome-font/c/dc027189e0ba4cd.woff"

def fetch(url):
    response = requests.get(url, timeout=45)
    response.raise_for_status()
    return response.content

def signature(glyphs, name):
    pen = RecordingPen()
    glyphs[name].draw(pen)
    return hashlib.sha256(repr(pen.value).encode()).hexdigest()

def priority(character):
    code = ord(character)
    return (0 if code < 128 else 1 if 0x4E00 <= code <= 0x9FFF else 2, code)

if __name__ == "__main__":
    source_raw, subset_raw = fetch(SOURCE), fetch(SUBSET)
    source, subset = TTFont(io.BytesIO(source_raw)), TTFont(io.BytesIO(subset_raw))
    source_glyphs, subset_glyphs = source.getGlyphSet(), subset.getGlyphSet()
    needed = {signature(subset_glyphs, name): code for code, name in subset.getBestCmap().items()}
    matches = {}
    for code, name in source.getBestCmap().items():
        digest = signature(source_glyphs, name)
        if digest in needed:
            matches.setdefault(needed[digest], []).append(chr(code))
    if len(matches) != len(needed):
        raise RuntimeError(f"incomplete font mapping: {len(matches)}/{len(needed)}")
    mapping = {chr(code): min(choices, key=priority) for code, choices in matches.items()}
    (OUT / "font-dc027189e0ba4cd-derived.json").write_text(json.dumps(mapping, ensure_ascii=False, indent=2), encoding="utf-8")
    provenance = dict(
        method="SHA256 of exact decomposed glyph path commands; independently matched Adobe SourceHanSansSC-Normal 2.002R and public font subset",
        sourceUrl=SOURCE, sourceSha256=hashlib.sha256(source_raw).hexdigest(),
        subsetUrl=SUBSET, subsetSha256=hashlib.sha256(subset_raw).hexdigest(),
        matched=len(mapping), total=len(needed),
        ambiguityPolicy="prefer ASCII Latin/digits, then basic CJK U+4E00..U+9FFF, then lowest codepoint",
        ambiguous={hex(code): choices for code, choices in matches.items() if len(choices)>1},
        licenseMetadata=[record.toUnicode() for record in subset["name"].names if record.nameID in (0, 13, 14)],
    )
    (OUT / "font-derivation.json").write_text(json.dumps(provenance, ensure_ascii=False, indent=2), encoding="utf-8")
    print(f"derived {len(mapping)} exact glyph mappings")
