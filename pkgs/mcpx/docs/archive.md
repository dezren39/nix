# Archive

Where retired entries go.

`docs/release-runbook.md` says never to delete a feature entry: either strike
its heading through and set `status: retired`, or move the whole entry here and
leave a one-line stub in the feature list pointing at it. Heavy refactors move
the old text here too.

This file is **append-only and unordered**. It is not a changelog and nothing
reads it in sequence; it exists so that a question about why something was
removed has an answer that is still in the tree, rather than only in a squashed
commit message.

An entry keeps the key block it had when it was live, plus the reason it was
retired and what replaced it.

It is empty because nothing has been retired yet. The runbook named it before
it existed, which is the documentation form of a capability declared and not
delivered; `TestEveryDocumentationLinkResolves` in `internal/defaults` is what
noticed, and what will notice the next one.
