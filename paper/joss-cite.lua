-- pandoc <3.2 reads typst @key as Link{.ref}; turn those into citations
-- and merge adjacent ones, so "@a @b" becomes [@a; @b] as JOSS expects.
-- ponytail: no-op on newer pandoc, which emits Cite itself.
function Link(l)
  if l.classes:includes("ref") then
    local key = l.target:gsub("^#", "")
    return pandoc.Cite({}, {pandoc.Citation(key, "NormalCitation")})
  end
end

function Inlines(xs)
  local out = pandoc.Inlines{}
  for _, x in ipairs(xs) do
    local n = #out
    if x.t == "Cite" and n >= 2 and out[n].t == "Space" and out[n-1].t == "Cite" then
      out:remove(n)
      out[n-1].citations:extend(x.citations)
    else
      out:insert(x)
    end
  end
  return out
end
