import { t } from "../i18n.js";
import { icon } from "../icon.js";
import { getCurrentUser } from "../session.js";
import { WEBSITE_URL, REPO_URL, ISSUES_URL } from "../project-links.js";
import { contentDialog } from "./dialog.js";

// The About dialog, from the user menu: what this is, which build of it is
// running, and where it comes from.
//
// The version is the one /auth/me reported at boot, not a fresh request: it
// cannot change under a running page, and a dialog that needed the network to
// say which build it is would fail exactly when somebody is trying to report
// a problem.
//
// Built with DOM APIs and textContent throughout. Nothing here is user data,
// but the translations are not markup either, and this way neither can become
// it.
export function openAboutDialog() {
  const content = document.createElement("div");
  content.className = "about-dialog__body";

  // The same lockup as the login hero, so the dialog reads as the app's own.
  const brand = document.createElement("span");
  brand.className = "app-brand about-dialog__brand";
  brand.innerHTML = `<span class="brand-mark" aria-hidden="true"></span><span class="app-brand__wordmark"></span>`;
  brand.querySelector(".app-brand__wordmark").textContent = t("app.name");
  content.appendChild(brand);

  // The hero's own tagline: one slogan, said the same way in both places.
  const tagline = document.createElement("p");
  tagline.className = "about-dialog__tagline";
  tagline.textContent = t("auth.hero.title");
  content.appendChild(tagline);

  const subtitle = document.createElement("p");
  subtitle.className = "about-dialog__subtitle";
  subtitle.textContent = t("auth.hero.subtitle");
  content.appendChild(subtitle);

  const build = getCurrentUser()?.build;
  if (build?.version) {
    const version = document.createElement("p");
    version.className = "about-dialog__version";
    const label = document.createElement("span");
    label.textContent = `${t("about.version")} `;
    version.appendChild(label);
    // Linked only when the server could say where to look it up: a dev or
    // -dirty build names no release and no commit, and a link to the nearest
    // one would claim a build this is not.
    const value = build.url ? externalLink(build.url) : document.createElement("span");
    value.classList.add("about-dialog__version-value");
    value.textContent = build.version;
    version.appendChild(value);
    content.appendChild(version);
  }

  const links = document.createElement("ul");
  links.className = "about-dialog__links";
  for (const { href, iconName, labelKey } of [
    { href: WEBSITE_URL, iconName: "book-open", labelKey: "about.website" },
    { href: REPO_URL, iconName: "code", labelKey: "about.github" },
    { href: ISSUES_URL, iconName: "bug", labelKey: "about.reportProblem" },
  ]) {
    const item = document.createElement("li");
    const a = externalLink(href);
    a.className = "about-dialog__link";
    a.innerHTML = `${icon(iconName)} <span></span>`;
    a.querySelector("span").textContent = t(labelKey);
    item.appendChild(a);
    links.appendChild(item);
  }
  content.appendChild(links);

  return contentDialog({
    messageKey: "about.title",
    hideMessage: true,
    content,
    className: "about-dialog",
  });
}

// A new tab, for every link here: the dialog is a detour from the trip the
// user was on, and following a link should not end it.
function externalLink(href) {
  const a = document.createElement("a");
  a.href = href;
  a.target = "_blank";
  a.rel = "noopener noreferrer";
  return a;
}
