// The first spec that *writes*.
//
// Everything else in this suite either sweeps rendered pages or drives an
// interaction that mutates nothing, because every spec runs against the one
// shared seed and a write would leak into the others (see todo.md). This one
// takes the isolation route that entry suggests as the cheapest: it creates its
// own trip, does all its damage there, and deletes it afterwards - the seeded
// scenarios the other specs depend on are never touched.
//
// What it covers is the whole Files lifecycle, which is exactly the part no
// assertion reached before: upload through the drop zone, several files in one
// gesture, the size guard, ⋮ → Edit note (including that the note becomes the
// card's title), and ⋮ → Delete behind its confirm dialog.
import { test, expect } from "@playwright/test";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { login } from "./helpers/scenarios.js";

// The harder width on purpose: at 324px the card's meta line wraps, the size
// column is the inline copy, and the drop zone shows its narrow copy.
const MOBILE = { width: 324, height: 756 };

test.describe("files tab, end to end", () => {
  test.use({ viewport: MOBILE });

  let tripId;
  // Playwright refuses an in-memory buffer over 50MB, which is precisely the
  // size the limit check needs, so that one goes to disk - sparse (ftruncate),
  // so it costs a byte count and not 51MB of writing. Its batch-mate has to go
  // to disk with it: setInputFiles takes paths or buffers, never both.
  let tmpDir;
  let hugePath;
  let okPath;

  test.beforeAll(() => {
    tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "caravel-files-spec-"));
    hugePath = path.join(tmpDir, "huge-map.pdf");
    const fd = fs.openSync(hugePath, "w");
    fs.ftruncateSync(fd, 51 * 1024 * 1024);
    fs.closeSync(fd);
    okPath = path.join(tmpDir, "small-ok.txt");
    fs.writeFileSync(okPath, "ok");
  });

  test.afterAll(() => {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  });

  test.beforeEach(async ({ page }) => {
    await login(page);
    const res = await page.request.post("/api/trips", { data: { title: "UI suite: files spec" } });
    expect(res.status(), "create the spec's own trip").toBe(201);
    tripId = (await res.json()).id;
  });

  test.afterEach(async ({ page }) => {
    // Cascades to the files, and to their blobs via the delete handler. Runs
    // even when the test failed, so a red run doesn't leave litter behind for
    // the next one to trip over.
    if (tripId) await page.request.delete(`/api/trips/${tripId}`);
    tripId = null;
  });

  test("uploads through the drop zone, edits a note, and deletes", async ({ page }) => {
    await page.goto(`/trips/${tripId}/files`);

    const cards = page.locator(".file-card");
    const summary = page.locator(".file-list__summary");
    const error = page.locator(".file-list__error");
    const drop = page.locator(".file-drop");

    // Empty to begin with: no summary line, no cards, and the empty state
    // saying so.
    await expect(page.locator(".file-list-empty")).toBeVisible();
    await expect(summary).toBeHidden();
    await expect(cards).toHaveCount(0);

    // The zone is a label for a hidden input, so this is the browse path -
    // Playwright sets the files and the change handler does the rest. Two at
    // once, which the old single-file add row could not do at all.
    await drop.locator('input[type="file"]').setInputFiles([
      { name: "ferry-ticket.pdf", mimeType: "application/pdf", buffer: Buffer.from("a ferry ticket") },
      { name: "campsite-notes.txt", mimeType: "text/plain", buffer: Buffer.from("notes") },
    ]);

    await expect(cards).toHaveCount(2);
    await expect(error).toBeHidden();
    // 14 + 5 bytes, and the plural form of the summary.
    await expect(summary).toHaveText("2 files · 19 B");

    // Newest first - the order the API returns and, since Milestone 5, the
    // order a freshly uploaded file lands in too. With no note the filename is
    // the card title.
    const first = cards.first();
    await expect(first.locator(".file-card__name")).toHaveText("campsite-notes.txt");
    await expect(first.locator(".file-card__nonote")).toHaveAttribute("class", /nonote/);
    // The card body is the download link, and it is the tap target for the
    // whole card - the thing the old filename-only link was not.
    const body = first.locator(".file-card__body");
    await expect(body).toHaveAttribute("href", /^\/api\/files\/[0-9a-f-]+\/download$/);
    const box = await body.boundingBox();
    expect(box.height, "card body height").toBeGreaterThanOrEqual(44);

    // A file over the server's 50 MB limit is refused by name, client-side,
    // and the rest of its batch still lands: a batch that failed anonymously
    // would be the easy thing to build here and the useless one to have.
    await drop.locator('input[type="file"]').setInputFiles([
      hugePath,
      okPath,
    ]);
    await expect(error).toBeVisible();
    // The exact client-side copy, not merely the filename: with the guard
    // removed the server answers 413 and the catch below it reports
    // "huge-map.pdf: file too large or invalid multipart form" - which also
    // contains the filename, so a looser assertion passed either way and
    // proved nothing about the guard.
    await expect(error).toHaveText("huge-map.pdf is too large — the limit is 50.0 MB.");
    await expect(cards).toHaveCount(3);
    await expect(cards.first().locator(".file-card__name")).toHaveText("small-ok.txt");

    // ⋮ → Edit note. The note then *is* the card's title, and the filename
    // moves down to the meta line - the rule this redesign turns on.
    await cards.first().locator(".menu__trigger").click();
    await cards.first().getByRole("menuitem", { name: "Edit note" }).click();
    const noteField = page.locator(".dialog__input");
    await expect(noteField).toBeFocused();
    await noteField.fill("Boarding pass");
    await page.locator(".dialog__actions button", { hasText: "Save" }).click();

    await expect(cards.first().locator(".file-card__name")).toHaveText("Boarding pass");
    await expect(cards.first().locator(".file-card__meta")).toContainText("small-ok.txt");
    await expect(cards.first().locator(".file-card__nonote")).toHaveCount(0);

    // It survives a reload, i.e. it reached the database rather than only the
    // rendered list.
    await page.reload();
    await expect(cards.first().locator(".file-card__name")).toHaveText("Boarding pass");

    // ⋮ → Delete, behind the confirm dialog. Cancel first: a destructive
    // action that fires on Cancel is the bug worth catching here.
    await cards.first().locator(".menu__trigger").click();
    await cards.first().getByRole("menuitem", { name: "Delete" }).click();
    await page.locator(".dialog__actions button", { hasText: "Cancel" }).click();
    await expect(cards).toHaveCount(3);

    await cards.first().locator(".menu__trigger").click();
    await cards.first().getByRole("menuitem", { name: "Delete" }).click();
    await page.locator(".dialog__actions button", { hasText: "Delete" }).click();
    await expect(cards).toHaveCount(2);
    await expect(summary).toHaveText("2 files · 19 B");
  });
});
