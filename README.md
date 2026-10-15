# tiller

A personal command centre over a folder of org files: journal, zettelkasten
notes and goals. Plain text stays the source of truth and syncs however you
like (syncthing here); tiller reads it, shows you what needs attention, and
hands editing to nvim.

    go install ./cmd/tiller

    tiller                 home screen
    tiller jot [text]      append to today's journal, or open it
    tiller new <title>     new fleeting note
    tiller graph           link graph page in the browser
    tiller quote           today's quote
    tiller migrate [-n]    one-shot conversion from the old layout

Root is `$TILLER_ROOT`, else `~/orgfiles`. Editor is `$EDITOR`, else nvim.
Press `?` in the app for the key panel.
