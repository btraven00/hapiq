// Preprint (bioRxiv) layout. Content lives in body.typ, metadata in meta.yaml,
// so the JOSS paper.md is generated from the same sources (see Makefile).
#let meta = yaml("meta.yaml")

#set document(title: meta.title)
#set page(paper: "a4", margin: 2.2cm, numbering: "1")
#set text(font: "Libertinus Serif", size: 11pt)
#set par(justify: true)
#set heading(numbering: "1.")
#show raw.where(block: true): set text(size: 9pt)
#show link: set text(fill: rgb("#1a4f8b"))

#align(center)[
  #text(17pt, weight: "bold", meta.title)
  #v(0.6em)
  #meta.authors.map(a => [#a.name#super(str(a.affiliation))#if "orcid" in a [ #link("https://orcid.org/" + a.orcid)[#text(8pt, fill: rgb("#a6ce39"))[ORCID]]]]).join(", ")
  #v(0.3em)
  #text(9pt)[#meta.affiliations.map(f => [#super(str(f.index))#f.name]).join(linebreak())]
  #v(0.3em)
  #text(9pt, meta.date)
]
#v(1em)

#include "body.typ"

#bibliography("paper.bib", style: "apa")
