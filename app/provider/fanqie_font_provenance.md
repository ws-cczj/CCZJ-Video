# Fanqie public-web font alphabet

`fanqie_font.json` maps the private-use code points of the public web font
revision `dc027189e0ba4cd` to the same glyphs in Adobe Source Han Sans SC Normal
2.002R. The adapter recognizes this exact font revision and rejects unknown
revisions/code points rather than caching incorrectly decoded text.

The table was derived independently from exact decomposed outline-command
hashes, using fontTools RecordingPen, without copying another project's mapping
or running source-supplied JavaScript. All 362 glyphs matched. Identical outlines
can represent multiple Unicode characters; the deterministic preference is ASCII,
then basic CJK, then the lowest code point. This alphabet does not establish
semantic differences between visually identical characters.

- Original font: [Adobe Source Han Sans 2.002R](https://github.com/adobe-fonts/source-han-sans/tree/2.002R), SIL Open Font License 1.1.
- Original font SHA-256: `69a090057ee75936451397c038da6d32194e45bbf878ef674dddc0d5f0621ce5`.
- Public subset: `https://lf6-awef.bytetos.com/obj/awesome-font/c/dc027189e0ba4cd.woff`.
- Subset SHA-256: `ba1a867e395a7f8bca99dd13a0d6428b8f12fc9c38e3a0b2c3522b98f41763fe`.

Reproduction and observations are kept in
`reports/fanqie-adapter-samples-2026-10-19/derive_font_map.py` and
`font-derivation.json`. No font binary is bundled with the application.

## Legacy GBK characters in public chapter text

2026-10-22: the public second chapter of *Journey to the West*
(`7345722494013148201`) also contains U+E82F, which is absent from the
362-character webfont cmap. This is a historical GBK private-use character,
not a new font revision. The original font's SHA-256 still matches the value
above. [W3C encoding correspondence](https://lists.w3.org/Archives/Public/www-archive/2013Dec/0010.html)
and the [WHATWG GB18030 index](https://encoding.spec.whatwg.org/index-gb18030.txt)
identify FE6A as U+3CE0 (㳠).

The decoder first uses the recognized font alphabet, then permits the exact
historical GBK block U+E815..U+E864 through the existing pinned GB18030 codec.
It converts only mappings to a non-PUA Unicode character; an unrecognized font,
unmapped private-use character or locked chapter retains its explicit failure.
No guessed character, stripped character or replacement prose enters the cache.

`TestFanqieClassicChapterLegacyGBKCharacterIsNotFontObfuscation` reproduced
the original failure before the fix. Opt-in live verification is
`CCZJ_FANQIE_LIVE=1 go test ./app/provider -run TestFanqieLivePublicClassicChapters -v`;
on Windows set the environment variable in PowerShell first and remove it after
the command. The first three public chapters passed with 6,970, 7,084 and 7,087
decoded characters and zero remaining PUA characters. Local response captures
and logs are temporary artifacts under `dist/audit/fanqie-font/`.
