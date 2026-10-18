// Chain two servers in one script: library docs plus the local code index.
//
//   mcpx run chain-docs-and-code.ts <libraryId> "<docs query>"
//
// This is the chaining pattern from SKILL.md section 2, kept in a single
// invocation so neither server's full result crosses into the AGY
// transcript: the script prints the indexed-file count and only the
// first documentation snippet. Promise.allSettled, so a server that is
// not configured reports its failure without hiding the other answer.
const [libraryId, query] = Deno.args;
if (!libraryId || !query) {
  console.error('usage: mcpx run chain-docs-and-code.ts <libraryId> "<docs query>"');
  Deno.exit(2);
}
const [status, doc] = await Promise.allSettled([
  tools.codedb.status({}),
  tools.context7.query_docs({ libraryId, query }),
]);
if (status.status === "fulfilled") {
  console.log("Indexed files:", (status.value as any)?.fileCount ?? "unknown");
} else {
  console.log("codedb status failed:", (status.reason as Error).message);
}
if (doc.status === "fulfilled") {
  const results = (doc.value as any)?.results;
  console.log(results?.[0]?.snippet ?? "Not found");
} else {
  console.log("context7 query failed:", (doc.reason as Error).message);
}
