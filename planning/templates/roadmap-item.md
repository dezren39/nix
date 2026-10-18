# Roadmap item template

Copy the block into the roadmap file for the project, under the horizon it belongs to.
An item can start as a single line. Add the other fields as they become known.

```markdown
| Title      | Status | Intent                                                            | Links                             |
| ---------- | ------ | ----------------------------------------------------------------- | --------------------------------- |
| Short name | idea   | One sentence: what a user or maintainer gets, and why it matters. | #123, the design record, the plan |
```

Fields:

- **Status** is one of `idea`, `proposed`, `shaped`, `planned`, `in-progress`, `shipped`, `dropped`. See the README for what each one means.
- **Intent** says what changes for someone. Leave out the how until there is a design.
- **Links** point to issues, design records, plans and PRs, in that order of appearance.

When an item ships or is dropped, move its row to the "Done" table at the bottom of the file with the date and the PR or reason. Do not delete rows.
