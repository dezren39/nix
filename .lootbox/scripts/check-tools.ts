const status = await tools.mcp_codedb.codedb_status({});

console.log(JSON.stringify(status, null, 2));
