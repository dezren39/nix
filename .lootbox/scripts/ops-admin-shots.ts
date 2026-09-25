const ROOT =
  "/Users/drewry.pope/.local/share/opencode/worktree/8a316cd49cd3cf82f0b98d015203c39347e50084/lucky-falcon";
const B = "http://127.0.0.1:8137";
const cd = tools.mcp_chrome_devtools;
const wait = (ms: number) => new Promise((r) => setTimeout(r, ms));
const text = (r: any) => String(r?.content?.[0]?.text ?? "");

await cd.navigate_page({ pageId: 1, url: `${B}/admin/storage-status` });
await wait(3000);

let snap = text(await cd.take_snapshot({ pageId: 1 }));
if (/Login \| Operations Portal/.test(snap)) {
  const ids: Record<string, string> = {};
  for (const l of snap.split("\n")) {
    const m = l.match(/uid=(\S+)\s+(?:textbox|button)\s+"([^"]+)"/);
    if (m) ids[m[2]] = m[1];
  }
  await cd.fill({ pageId: 1, uid: ids["Username"], value: "admin" });
  await cd.fill({ pageId: 1, uid: ids["Password"], value: "Str0ng-Local-Dev-Password!42" });
  await cd.click({ pageId: 1, uid: ids["Log in"] });
  await wait(8000);
  snap = text(await cd.take_snapshot({ pageId: 1 }));
}
console.log("PAGE1:", snap.split("\n")[1]?.slice(0, 120));

await cd.take_screenshot({
  pageId: 1,
  format: "png",
  fullPage: true,
  filePath: `${ROOT}/.screenshots/storage-status.png`,
});

const lines = snap.split("\n");
const i = lines.findIndex((l: string) => / main$/.test(l));
console.log(lines.slice(i, i + 120).join("\n").slice(0, 4500));

await cd.navigate_page({ pageId: 1, url: `${B}/admin/sql` });
await wait(5000);
const snap2 = text(await cd.take_snapshot({ pageId: 1 }));
console.log("PAGE2:", snap2.split("\n")[1]?.slice(0, 120));
await cd.take_screenshot({
  pageId: 1,
  format: "png",
  fullPage: true,
  filePath: `${ROOT}/.screenshots/sql-console.png`,
});
const l2 = snap2.split("\n");
const j = l2.findIndex((l: string) => / main$/.test(l));
console.log(l2.slice(j, j + 120).join("\n").slice(0, 4500));
