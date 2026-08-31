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
// assertion reached before: staging a pick through the drop zone, several files
// in one gesture, taking one back, the size guard, the Upload button (and that
// a note typed after the pick lands on it), ⋮ → Edit note (including that the
// note becomes the card's title), and ⋮ → Delete behind its confirm dialog.
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

  test("stages a pick, uploads it on the button, edits a note, and deletes", async ({ page }) => {
    // Every upload the page makes, so "nothing is sent until the button" is an
    // assertion about the wire and not about what happens to be on screen.
    const uploads = [];
    page.on("request", (req) => {
      if (req.method() === "POST" && /\/api\/trips\/[0-9a-f-]+\/files$/.test(req.url())) uploads.push(req.url());
    });

    await page.goto(`/trips/${tripId}/files`);

    const cards = page.locator(".file-sections .file-card");
    const staged = page.locator(".files--pending .file-card");
    const uploadBtn = page.locator(".file-upload__submit");
    const summary = page.locator(".file-list__summary");
    const error = page.locator(".file-list__error");
    const drop = page.locator(".file-drop");
    const noteInput = page.locator('[name="uploadNote"]');

    // Empty to begin with: no summary line, no cards, the empty state saying
    // so, and neither the pending list nor the button - with nothing picked
    // there is nothing for either to be about.
    await expect(page.locator(".file-list-empty")).toBeVisible();
    await expect(summary).toBeHidden();
    await expect(cards).toHaveCount(0);
    await expect(page.locator(".file-upload__pending")).toBeHidden();
    await expect(uploadBtn).toBeHidden();

    // The zone is a label for a hidden input, so this is the browse path -
    // Playwright sets the files and the change handler does the rest. Two at
    // once, which the old single-file add row could not do at all.
    await drop.locator('input[type="file"]').setInputFiles([
      { name: "ferry-ticket.pdf", mimeType: "application/pdf", buffer: Buffer.from("a ferry ticket") },
      { name: "campsite-notes.txt", mimeType: "text/plain", buffer: Buffer.from("notes") },
    ]);

    // Staged, not uploaded. This is the point of the whole flow: the stored
    // list has not moved, nothing has been POSTed, and the button says how many
    // files are waiting.
    await expect(staged).toHaveCount(2);
    await expect(cards).toHaveCount(0);
    await expect(summary).toBeHidden();
    await expect(uploadBtn).toHaveText("Upload 2 files");
    expect(uploads, "nothing uploaded by picking").toHaveLength(0);
    // Newest first here too, so the row you just picked is the one at the top.
    await expect(staged.first().locator(".file-card__name")).toHaveText("campsite-notes.txt");
    // A staged pick has nothing to download yet, so its body is not a link.
    await expect(staged.first().locator("a.file-card__body")).toHaveCount(0);

    // A pick can be taken back, which upload-on-select could only offer as a
    // delete of something already stored. One action per row, so an icon and
    // not a menu.
    await staged.first().locator(".icon-remove").click();
    await expect(staged).toHaveCount(1);
    await expect(staged.first().locator(".file-card__name")).toHaveText("ferry-ticket.pdf");
    await expect(uploadBtn).toHaveText("Upload 1 file");
    expect(uploads, "nothing uploaded by removing").toHaveLength(0);

    // The note is typed *after* the file was picked and still lands on it -
    // the thing that was impossible when picking was the upload. Same path
    // carries the visibility choice on a shared trip (see menu.spec.js, which
    // covers the radios; this spec's trip is solo, so it has none).
    await noteInput.fill("Boarding pass");
    await uploadBtn.click();

    await expect(cards).toHaveCount(1);
    await expect(staged).toHaveCount(0);
    await expect(uploadBtn).toBeHidden();
    await expect(error).toBeHidden();
    expect(uploads, "one POST per file, on the button").toHaveLength(1);
    // The note wins the title and the filename drops to the meta line.
    await expect(cards.first().locator(".file-card__name")).toHaveText("Boarding pass");
    await expect(cards.first().locator(".file-card__meta")).toContainText("ferry-ticket.pdf");
    await expect(summary).toHaveText("1 file · 14 B");
    // The card body is the download link, and it is the tap target for the
    // whole card - the thing the old filename-only link was not.
    const body = cards.first().locator(".file-card__body");
    await expect(body).toHaveAttribute("href", /^\/api\/files\/[0-9a-f-]+\/download$/);
    const box = await body.boundingBox();
    expect(box.height, "card body height").toBeGreaterThanOrEqual(44);
    // The note was consumed by the batch that used it, so the next upload does
    // not silently inherit it.
    await expect(noteInput).toHaveValue("");

    // A file over the server's 50 MB limit is refused by name, client-side, at
    // *pick* time - so it never reaches the wire at all - and the rest of its
    // batch still stages: a batch that failed anonymously would be the easy
    // thing to build here and the useless one to have.
    await drop.locator('input[type="file"]').setInputFiles([hugePath, okPath]);
    await expect(error).toBeVisible();
    // The exact client-side copy, not merely the filename: with the guard
    // removed the server answers 413 and the catch below it reports
    // "huge-map.pdf: file too large or invalid multipart form" - which also
    // contains the filename, so a looser assertion passed either way and
    // proved nothing about the guard.
    await expect(error).toHaveText("huge-map.pdf is too large — the limit is 50.0 MB.");
    await expect(staged).toHaveCount(1);
    await expect(staged.first().locator(".file-card__name")).toHaveText("small-ok.txt");
    expect(uploads, "the oversized file was never sent").toHaveLength(1);

    await uploadBtn.click();
    await expect(cards).toHaveCount(2);
    expect(uploads).toHaveLength(2);
    // Newest first - the order the API returns and the order a freshly
    // uploaded file lands in too. With no note the filename is the card title.
    const first = cards.first();
    await expect(first.locator(".file-card__name")).toHaveText("small-ok.txt");
    await expect(first.locator(".file-card__nonote")).toHaveAttribute("class", /nonote/);
    // 14 + 2 bytes, and the plural form of the summary.
    await expect(summary).toHaveText("2 files · 16 B");

    // ⋮ → Edit note, the post-upload half: a note can still be changed after
    // the fact, and it then *is* the card's title.
    await first.locator(".menu__trigger").click();
    await first.getByRole("menuitem", { name: "Edit note" }).click();
    const noteField = page.locator(".dialog__input");
    await expect(noteField).toBeFocused();
    await noteField.fill("Campsite map");
    await page.locator(".dialog__actions button", { hasText: "Save" }).click();

    await expect(cards.first().locator(".file-card__name")).toHaveText("Campsite map");
    await expect(cards.first().locator(".file-card__meta")).toContainText("small-ok.txt");
    await expect(cards.first().locator(".file-card__nonote")).toHaveCount(0);

    // Both notes survive a reload, i.e. they reached the database rather than
    // only the rendered list - the one PATCHed after upload and the one that
    // travelled with the upload itself.
    await page.reload();
    await expect(cards.first().locator(".file-card__name")).toHaveText("Campsite map");
    await expect(cards.nth(1).locator(".file-card__name")).toHaveText("Boarding pass");

    // ⋮ → Delete, behind the confirm dialog. Cancel first: a destructive
    // action that fires on Cancel is the bug worth catching here.
    await cards.first().locator(".menu__trigger").click();
    await cards.first().getByRole("menuitem", { name: "Delete" }).click();
    await page.locator(".dialog__actions button", { hasText: "Cancel" }).click();
    await expect(cards).toHaveCount(2);

    await cards.first().locator(".menu__trigger").click();
    await cards.first().getByRole("menuitem", { name: "Delete" }).click();
    await page.locator(".dialog__actions button", { hasText: "Delete" }).click();
    await expect(cards).toHaveCount(1);
    await expect(summary).toHaveText("1 file · 14 B");
  });
});
