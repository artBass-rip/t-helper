#!/usr/bin/env node

const fs = require("fs");
const path = require("path");

const docsRoot = __dirname;
const args = process.argv.slice(2);
const outIndex = args.indexOf("--out");
const outRoot =
  outIndex >= 0 && args[outIndex + 1]
    ? path.resolve(process.cwd(), args[outIndex + 1])
    : docsRoot;

const markdownFiles = [];
let renderContext = {
  currentDir: "",
  docsByRel: new Map(),
  lang: "ru",
};

const languages = {
  ru: {
    code: "ru",
    root: "",
    home: "index.html",
    skip: "К содержанию",
    navAria: "Навигация документации",
    navSectionsAria: "Разделы документации",
    docs: "Документы",
    roadmap: "Roadmap",
    source: ".md",
    asideAria: "Навигация по документу",
    homeLabel: "T-Helper Docs",
    globalAria: "Ключевые документы",
    tocFallback: "Начало",
    related: "Связанные документы",
    referencedBy: "Ссылаются сюда",
    relationsAria: "Перекрёстные ссылки",
    fallbackTitle: "Навигация",
    fallbackText: "Этот документ входит в общий каталог T-Helper Docs.",
    directoryAria: "Каталог документации",
    docsMap: "Docs map",
    allPages: "Все страницы документации",
    sourceLabel: "Исходный Markdown",
    ruPage: "Русская страница",
    enPage: "English page",
    groupLabels: {
      "Core Docs": "Core Docs",
      "Implementation Specs": "Implementation Specs",
      ADR: "ADR",
    },
  },
  en: {
    code: "en",
    root: "en",
    home: "en.html",
    skip: "Skip to content",
    navAria: "Documentation navigation",
    navSectionsAria: "Documentation sections",
    docs: "Docs",
    roadmap: "Roadmap",
    source: ".md",
    asideAria: "Document navigation",
    homeLabel: "T-Helper Docs",
    globalAria: "Key documents",
    tocFallback: "Top",
    related: "Related documents",
    referencedBy: "Referenced by",
    relationsAria: "Cross references",
    fallbackTitle: "Navigation",
    fallbackText: "This document is part of the T-Helper Docs catalog.",
    directoryAria: "Documentation catalog",
    docsMap: "Docs map",
    allPages: "All documentation pages",
    sourceLabel: "Source Markdown",
    ruPage: "Русская страница",
    enPage: "English page",
    groupLabels: {
      "Core Docs": "Core Docs",
      "Implementation Specs": "Implementation Specs",
      ADR: "ADR",
    },
  },
};

function walk(dir) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (entry.name === ".git") {
      continue;
    }

    const fullPath = path.join(dir, entry.name);

    if (entry.isDirectory()) {
      walk(fullPath);
      continue;
    }

    if (entry.isFile() && entry.name.endsWith(".md")) {
      markdownFiles.push(fullPath);
    }
  }
}

function copyTree(source, target) {
  fs.mkdirSync(target, { recursive: true });

  for (const entry of fs.readdirSync(source, { withFileTypes: true })) {
    const sourcePath = path.join(source, entry.name);
    const targetPath = path.join(target, entry.name);

    if (entry.isDirectory()) {
      copyTree(sourcePath, targetPath);
      continue;
    }

    fs.copyFileSync(sourcePath, targetPath);
  }
}

function escapeHtml(value) {
  return value
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

function slugify(value, usedIds) {
  const base =
    value
      .toLowerCase()
      .replace(/`([^`]+)`/g, "$1")
      .replace(/<[^>]+>/g, "")
      .replace(/[^\p{L}\p{N}\s-]/gu, "")
      .trim()
      .replace(/\s+/g, "-") || "section";

  let id = base;
  let suffix = 2;

  while (usedIds.has(id)) {
    id = `${base}-${suffix}`;
    suffix += 1;
  }

  usedIds.add(id);
  return id;
}

function splitLink(href) {
  const hashIndex = href.indexOf("#");
  const queryIndex = href.indexOf("?");
  const cutIndex =
    hashIndex >= 0 && queryIndex >= 0
      ? Math.min(hashIndex, queryIndex)
      : Math.max(hashIndex, queryIndex);

  if (cutIndex < 0) {
    return { base: href, suffix: "" };
  }

  return { base: href.slice(0, cutIndex), suffix: href.slice(cutIndex) };
}

function isExternalHref(href) {
  return (
    href.startsWith("#") ||
    href.startsWith("http://") ||
    href.startsWith("https://") ||
    href.startsWith("mailto:") ||
    href.startsWith("tel:")
  );
}

function markdownHrefToHtml(href) {
  if (isExternalHref(href)) {
    return href;
  }

  const { base, suffix } = splitLink(href);

  if (!base.endsWith(".md")) {
    return href;
  }

  return `${base.slice(0, -3)}.html${suffix}`;
}

function toPosix(value) {
  return value.split(path.sep).join("/");
}

function normalizeDocRef(value, currentDir = "") {
  const cleanValue = value.replace(/^["'`]+|["'`.,;:!?]+$/g, "");

  if (!cleanValue.endsWith(".md")) {
    return "";
  }

  if (cleanValue.startsWith("docs/")) {
    return path.posix.normalize(cleanValue.slice("docs/".length));
  }

  if (cleanValue.startsWith("./") || cleanValue.startsWith("../")) {
    return path.posix.normalize(path.posix.join(currentDir || ".", cleanValue));
  }

  if (renderContext.docsByRel.has(cleanValue)) {
    return cleanValue;
  }

  const relativeCandidate = path.posix.normalize(path.posix.join(currentDir || ".", cleanValue));
  if (renderContext.docsByRel.has(relativeCandidate)) {
    return relativeCandidate;
  }

  return cleanValue;
}

function relativeHtmlHref(fromMdPath, toMdPath) {
  const fromDir = path.posix.dirname(toPosix(fromMdPath));
  const normalizedFromDir = fromDir === "." ? "" : fromDir;
  const targetHtml = toPosix(toMdPath).replace(/\.md$/, ".html");
  let href = path.posix.relative(normalizedFromDir || ".", targetHtml);

  if (!href.startsWith(".") && !href.startsWith("/")) {
    return href;
  }

  return href || path.posix.basename(targetHtml);
}

function htmlPathForDoc(relativeMdPath, lang = "ru") {
  const relativeHtmlPath = toPosix(relativeMdPath).replace(/\.md$/, ".html");
  return lang === "en" ? path.posix.join("en", relativeHtmlPath) : relativeHtmlPath;
}

function currentHtmlDir(relativeMdPath, lang = "ru") {
  const dir = path.posix.dirname(htmlPathForDoc(relativeMdPath, lang));
  return dir === "." ? "" : dir;
}

function relativeHtmlHrefForLang(fromMdPath, toMdPath, lang = "ru") {
  const fromDir = currentHtmlDir(fromMdPath, lang);
  const targetHtml = htmlPathForDoc(toMdPath, lang);
  const href = path.posix.relative(fromDir || ".", targetHtml);

  if (!href.startsWith(".") && !href.startsWith("/")) {
    return href;
  }

  return href || path.posix.basename(targetHtml);
}

function relativeDocPageHref(fromMdPath, fromLang, toLang) {
  const fromDir = currentHtmlDir(fromMdPath, fromLang);
  const targetHtml = htmlPathForDoc(fromMdPath, toLang);
  const href = path.posix.relative(fromDir || ".", targetHtml);

  if (!href.startsWith(".") && !href.startsWith("/")) {
    return href;
  }

  return href || path.posix.basename(targetHtml);
}

function assetPrefix(relativeMdPath, lang = "ru") {
  return `${lang === "en" ? "../" : ""}${relativePrefix(relativeMdPath)}`;
}

function homeHref(relativeMdPath, lang = "ru") {
  return `${assetPrefix(relativeMdPath, lang)}${languages[lang].home}`;
}

function docHrefFromCurrent(targetMdPath) {
  return relativeHtmlHrefForLang(renderContext.currentMdPath || "", targetMdPath, renderContext.lang || "ru");
}

function renderInline(value, options = {}) {
  const linkParts = [];
  const linkPattern = /\[([^\]]+)]\(([^)\s]+)(?:\s+"[^"]*")?\)/g;
  let text = value.replace(linkPattern, (_, label, href) => {
    const marker = `@@LINK${linkParts.length}@@`;
    const { base, suffix } = splitLink(href);
    const normalizedDoc = normalizeDocRef(base, renderContext.currentDir);
    const safeHref = renderContext.docsByRel.has(normalizedDoc)
      ? escapeHtml(`${docHrefFromCurrent(normalizedDoc)}${suffix}`)
      : escapeHtml(markdownHrefToHtml(href));

    linkParts.push(`<a href="${safeHref}">${renderInline(label, { suppressDocLinks: true })}</a>`);
    return marker;
  });

  const codeParts = [];
  text = escapeHtml(text).replace(/`([^`]+)`/g, (_, code) => {
    const marker = `@@CODE${codeParts.length}@@`;
    const targetDoc = normalizeDocRef(code, renderContext.currentDir);

    if (!options.suppressDocLinks && renderContext.docsByRel.has(targetDoc)) {
      codeParts.push(`<a href="${escapeHtml(docHrefFromCurrent(targetDoc))}"><code>${code}</code></a>`);
    } else {
      codeParts.push(`<code>${code}</code>`);
    }
    return marker;
  });

  text = text.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
  text = text.replace(/\*([^*]+)\*/g, "<em>$1</em>");

  codeParts.forEach((part, index) => {
    text = text.replace(`@@CODE${index}@@`, part);
  });
  linkParts.forEach((part, index) => {
    text = text.replace(`@@LINK${index}@@`, part);
  });

  return text;
}

function isTableSeparator(line) {
  return /^\s*\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)+\|?\s*$/.test(line);
}

function splitTableRow(line) {
  return line
    .trim()
    .replace(/^\|/, "")
    .replace(/\|$/, "")
    .split("|")
    .map((cell) => cell.trim());
}

function renderMarkdown(markdown) {
  const lines = markdown.replace(/\r\n/g, "\n").split("\n");
  const html = [];
  const headings = [];
  const usedIds = new Set();
  let title = "";
  let paragraph = [];
  let listType = null;
  let inCode = false;
  let codeLang = "";
  let codeLines = [];

  function flushParagraph() {
    if (paragraph.length === 0) {
      return;
    }

    html.push(`<p>${renderInline(paragraph.join(" "))}</p>`);
    paragraph = [];
  }

  function closeList() {
    if (!listType) {
      return;
    }

    html.push(`</${listType}>`);
    listType = null;
  }

  function flushCode() {
    const langClass = codeLang ? ` class="language-${escapeHtml(codeLang)}"` : "";
    html.push(`<pre><code${langClass}>${escapeHtml(codeLines.join("\n"))}</code></pre>`);
    codeLines = [];
    codeLang = "";
  }

  for (let index = 0; index < lines.length; index += 1) {
    const line = lines[index];

    if (inCode) {
      if (/^```/.test(line)) {
        inCode = false;
        flushCode();
      } else {
        codeLines.push(line);
      }
      continue;
    }

    const fence = line.match(/^```\s*([A-Za-z0-9_-]+)?\s*$/);
    if (fence) {
      flushParagraph();
      closeList();
      inCode = true;
      codeLang = fence[1] || "";
      continue;
    }

    if (!line.trim()) {
      flushParagraph();
      closeList();
      continue;
    }

    const heading = line.match(/^(#{1,6})\s+(.+)$/);
    if (heading) {
      flushParagraph();
      closeList();

      const level = heading[1].length;
      const headingText = heading[2].replace(/\s+#+\s*$/, "");
      const id = slugify(headingText, usedIds);
      const plainText = headingText.replace(/`([^`]+)`/g, "$1");

      if (!title && level === 1) {
        title = plainText;
      }

      headings.push({ level, text: plainText, id });
      html.push(`<h${level} id="${id}">${renderInline(headingText)}</h${level}>`);
      continue;
    }

    if (line.startsWith(">")) {
      flushParagraph();
      closeList();
      const quote = line.replace(/^>\s?/, "");
      html.push(`<blockquote><p>${renderInline(quote)}</p></blockquote>`);
      continue;
    }

    if (line.includes("|") && index + 1 < lines.length && isTableSeparator(lines[index + 1])) {
      flushParagraph();
      closeList();

      const headers = splitTableRow(line);
      index += 2;
      const rows = [];

      while (index < lines.length && lines[index].includes("|") && lines[index].trim()) {
        rows.push(splitTableRow(lines[index]));
        index += 1;
      }

      index -= 1;
      html.push("<table>");
      html.push(`<thead><tr>${headers.map((cell) => `<th>${renderInline(cell)}</th>`).join("")}</tr></thead>`);
      html.push("<tbody>");
      for (const row of rows) {
        html.push(`<tr>${row.map((cell) => `<td>${renderInline(cell)}</td>`).join("")}</tr>`);
      }
      html.push("</tbody></table>");
      continue;
    }

    const unordered = line.match(/^\s*[-*]\s+(.*)$/);
    const ordered = line.match(/^\s*\d+\.\s+(.*)$/);

    if (unordered || ordered) {
      flushParagraph();
      const nextType = unordered ? "ul" : "ol";

      if (listType && listType !== nextType) {
        closeList();
      }

      if (!listType) {
        listType = nextType;
        html.push(`<${listType}>`);
      }

      let item = unordered ? unordered[1] : ordered[1];

      while (index + 1 < lines.length) {
        const nextLine = lines[index + 1];

        if (
          !/^\s{2,}\S/.test(nextLine) ||
          /^```/.test(nextLine.trim()) ||
          /^(#{1,6})\s+/.test(nextLine.trim()) ||
          /^\s*[-*]\s+/.test(nextLine) ||
          /^\s*\d+\.\s+/.test(nextLine)
        ) {
          break;
        }

        item = `${item} ${nextLine.trim()}`;
        index += 1;
      }

      const task = item.match(/^\[([ xX])]\s+(.*)$/);

      if (task) {
        const checked = task[1].toLowerCase() === "x" ? " checked" : "";
        html.push(
          `<li class="task-item"><input type="checkbox"${checked} disabled> ${renderInline(task[2])}</li>`,
        );
      } else {
        html.push(`<li>${renderInline(item)}</li>`);
      }
      continue;
    }

    closeList();
    paragraph.push(line.trim());
  }

  if (inCode) {
    flushCode();
  }

  flushParagraph();
  closeList();

  return { body: html.join("\n"), headings, title };
}

function relativePrefix(relativeMdPath) {
  const depth = relativeMdPath.split(path.sep).length - 1;
  return depth === 0 ? "" : "../".repeat(depth);
}

function readableTitle(relativePath) {
  return path
    .basename(relativePath, ".md")
    .replace(/[-_]/g, " ")
    .replace(/\b\w/g, (letter) => letter.toUpperCase());
}

function extractTitle(markdown, relativePath) {
  const heading = markdown.match(/^#\s+(.+)$/m);

  if (!heading) {
    return readableTitle(relativePath);
  }

  return heading[1].replace(/\s+#+\s*$/, "").replace(/`([^`]+)`/g, "$1");
}

function docGroup(relativeMdPath) {
  if (relativeMdPath.startsWith("adr/")) {
    return "ADR";
  }

  if (relativeMdPath.startsWith("implementation-specs/")) {
    return "Implementation Specs";
  }

  return "Core Docs";
}

function extractReferencedDocs(markdown, relativeMdPath, docsByRel) {
  const currentDir = path.posix.dirname(toPosix(relativeMdPath));
  const normalizedDir = currentDir === "." ? "" : currentDir;
  const references = new Set();
  const withoutCodeBlocks = markdown.replace(/```[\s\S]*?```/g, "");
  const patterns = [
    /\[[^\]]+]\(([^)\s]+\.md(?:#[^)]+)?)\)/g,
    /`([^`]+\.md(?:#[^`]*)?)`/g,
    /\b((?:docs\/)?(?:adr\/|implementation-specs\/)?[A-Za-z0-9][A-Za-z0-9._/-]*\.md(?:#[A-Za-z0-9._-]+)?)\b/g,
  ];
  const previousContext = renderContext;

  renderContext = {
    currentDir: normalizedDir,
    currentMdPath: relativeMdPath,
    docsByRel,
    lang: "ru",
  };

  for (const pattern of patterns) {
    let match = pattern.exec(withoutCodeBlocks);

    while (match) {
      const { base } = splitLink(match[1]);
      const normalized = normalizeDocRef(base, normalizedDir);

      if (docsByRel.has(normalized) && normalized !== relativeMdPath) {
        references.add(normalized);
      }

      match = pattern.exec(withoutCodeBlocks);
    }
  }

  renderContext = previousContext;

  return [...references].sort();
}

function documentCatalog(currentMdPath, docs, lang = "ru") {
  const locale = languages[lang];
  const groups = ["Core Docs", "Implementation Specs", "ADR"];

  return groups
    .map((group) => {
      const items = docs.filter((doc) => doc.group === group);

      if (items.length === 0) {
        return "";
      }

      return `<section class="doc-directory-group">
          <h2>${locale.groupLabels[group] || group}</h2>
          <div class="doc-directory-links">
            ${items
              .map((doc) => {
                const current = doc.relativeMdPath === currentMdPath ? ' aria-current="page"' : "";
                const href = escapeHtml(relativeHtmlHrefForLang(currentMdPath, doc.relativeMdPath, lang));
                return `<a href="${href}"${current}>${escapeHtml(doc.title)}</a>`;
              })
              .join("\n")}
          </div>
        </section>`;
    })
    .join("\n");
}

function relationLinks(title, docs, currentMdPath, lang = "ru") {
  if (docs.length === 0) {
    return "";
  }

  return `<section>
          <h2>${escapeHtml(title)}</h2>
          <div class="doc-relation-links">
            ${docs
              .slice(0, 12)
              .map((doc) => {
                const href = escapeHtml(relativeHtmlHrefForLang(currentMdPath, doc.relativeMdPath, lang));
                return `<a href="${href}"><span>${escapeHtml(doc.group)}</span>${escapeHtml(doc.title)}</a>`;
              })
              .join("\n")}
          </div>
        </section>`;
}

function docShell({ relativeMdPath, title, body, headings, docs, outgoingDocs, incomingDocs, lang }) {
  const locale = languages[lang];
  const prefix = assetPrefix(relativeMdPath, lang);
  const sourceHref = `https://github.com/artBass-rip/t-helper/blob/master/docs/${relativeMdPath
    .split(path.sep)
    .join("/")}`;
  const toc = headings
    .filter((heading) => heading.level <= 3)
    .map(
      (heading) =>
        `<a class="toc-level-${heading.level}" href="#${heading.id}">${renderInline(heading.text)}</a>`,
    )
    .join("\n");
  const catalog = documentCatalog(relativeMdPath, docs, lang);
  const relations = [
    relationLinks(locale.related, outgoingDocs, relativeMdPath, lang),
    relationLinks(locale.referencedBy, incomingDocs, relativeMdPath, lang),
  ]
    .filter(Boolean)
    .join("\n");
  const ruHref = escapeHtml(relativeDocPageHref(relativeMdPath, lang, "ru"));
  const enHref = escapeHtml(relativeDocPageHref(relativeMdPath, lang, "en"));

  return `<!doctype html>
<html lang="${locale.code}">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <meta name="description" content="${escapeHtml(title)} - T-Helper documentation.">
    <title>${escapeHtml(title)} - T-Helper Docs</title>
    <link rel="alternate" hreflang="ru" href="${ruHref}">
    <link rel="alternate" hreflang="en" href="${enHref}">
    <link rel="stylesheet" href="${prefix}pages.css">
  </head>
  <body class="doc-page">
    <a class="skip-link" href="#main">${locale.skip}</a>

    <header class="site-header doc-header" aria-label="${locale.navAria}">
      <a class="brand" href="${homeHref(relativeMdPath, lang)}" aria-label="T-Helper">
        <span class="brand-mark" aria-hidden="true">T</span>
        <span>T-Helper</span>
      </a>
      <nav class="nav-links" aria-label="${locale.navSectionsAria}">
        <a href="${homeHref(relativeMdPath, lang)}#docs">${locale.docs}</a>
        <a href="${relativeHtmlHrefForLang(relativeMdPath, "requirements.md", lang)}">Requirements</a>
        <a href="${relativeHtmlHrefForLang(relativeMdPath, "architecture.md", lang)}">Architecture</a>
        <a href="${relativeHtmlHrefForLang(relativeMdPath, "roadmap.md", lang)}">${locale.roadmap}</a>
        <a href="${relativeHtmlHrefForLang(relativeMdPath, "api.md", lang)}">API</a>
      </nav>
      <div class="lang-switch doc-source">
        <a class="${lang === "ru" ? "is-active" : ""}" href="${ruHref}"${lang === "ru" ? ' aria-current="page"' : ""}>RU</a>
        <a class="${lang === "en" ? "is-active" : ""}" href="${enHref}"${lang === "en" ? ' aria-current="page"' : ""}>EN</a>
        <a class="source-link" href="${sourceHref}">${locale.source}</a>
      </div>
    </header>

    <main class="doc-shell" id="main">
      <aside class="doc-aside" aria-label="${locale.asideAria}">
        <a class="doc-home" href="${homeHref(relativeMdPath, lang)}">${locale.homeLabel}</a>
        <span>${escapeHtml(relativeMdPath.split(path.sep).join("/"))}</span>
        <nav class="doc-global-links" aria-label="${locale.globalAria}">
          <a href="${relativeHtmlHrefForLang(relativeMdPath, "requirements.md", lang)}">Requirements</a>
          <a href="${relativeHtmlHrefForLang(relativeMdPath, "architecture.md", lang)}">Architecture</a>
          <a href="${relativeHtmlHrefForLang(relativeMdPath, "api.md", lang)}">API</a>
          <a href="${relativeHtmlHrefForLang(relativeMdPath, "data-model.md", lang)}">Data model</a>
          <a href="${relativeHtmlHrefForLang(relativeMdPath, "roadmap.md", lang)}">Roadmap</a>
          <a href="${relativeHtmlHrefForLang(relativeMdPath, "traceability.md", lang)}">Traceability</a>
        </nav>
        <nav class="doc-toc">
          ${toc || `<a href="#main">${locale.tocFallback}</a>`}
        </nav>
      </aside>

      <div class="doc-content">
        <article class="markdown-body">
          ${body}
        </article>

        <section class="doc-relations" aria-label="${locale.relationsAria}">
          ${relations || `<section><h2>${locale.fallbackTitle}</h2><p>${locale.fallbackText}</p></section>`}
        </section>

        <section class="doc-directory" aria-label="${locale.directoryAria}">
          <div class="doc-directory-heading">
            <p class="eyebrow">${locale.docsMap}</p>
            <h2>${locale.allPages}</h2>
          </div>
          ${catalog}
        </section>
      </div>
    </main>

    <footer class="site-footer doc-footer">
      <span>T-Helper</span>
      <a href="${ruHref}">${locale.ruPage}</a>
      <a href="${enHref}">${locale.enPage}</a>
      <a href="${sourceHref}">${locale.sourceLabel}</a>
    </footer>

    <script src="${prefix}pages.js"></script>
  </body>
</html>
`;
}

function rewritePublishedHtmlLinks() {
  for (const htmlPath of findFiles(outRoot, ".html")) {
    const source = fs.readFileSync(htmlPath, "utf8");
    const relativeHtmlPath = toPosix(path.relative(outRoot, htmlPath));
    const isEnglishLanding = relativeHtmlPath === "en.html";
    const rewritten = source.replace(
      /(href=["'])([^"']+\.md(?:#[^"']*)?)(["'])/g,
      (_, start, href, end) => {
        const { base, suffix } = splitLink(href);
        const normalizedDoc = normalizeDocRef(base);

        if (isEnglishLanding && renderContext.docsByRel.has(normalizedDoc)) {
          return `${start}${escapeHtml(htmlPathForDoc(normalizedDoc, "en"))}${suffix}${end}`;
        }

        return `${start}${markdownHrefToHtml(href)}${end}`;
      },
    );

    if (rewritten !== source) {
      fs.writeFileSync(htmlPath, rewritten);
    }
  }
}

function findFiles(dir, extension) {
  const files = [];

  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const fullPath = path.join(dir, entry.name);

    if (entry.isDirectory()) {
      files.push(...findFiles(fullPath, extension));
      continue;
    }

    if (entry.isFile() && fullPath.endsWith(extension)) {
      files.push(fullPath);
    }
  }

  return files;
}

if (outRoot !== docsRoot) {
  copyTree(docsRoot, outRoot);
}

walk(docsRoot);

const docs = markdownFiles
  .map((mdPath) => {
    const relativeMdPath = toPosix(path.relative(docsRoot, mdPath));
    const markdown = fs.readFileSync(mdPath, "utf8");

    return {
      relativeMdPath,
      title: extractTitle(markdown, relativeMdPath),
      group: docGroup(relativeMdPath),
      markdown,
    };
  })
  .sort((left, right) => {
    const groupOrder = {
      "Core Docs": 1,
      "Implementation Specs": 2,
      ADR: 3,
    };

    if (groupOrder[left.group] !== groupOrder[right.group]) {
      return groupOrder[left.group] - groupOrder[right.group];
    }

    return left.relativeMdPath.localeCompare(right.relativeMdPath);
  });
const docsByRel = new Map(docs.map((doc) => [doc.relativeMdPath, doc]));

renderContext.docsByRel = docsByRel;

for (const doc of docs) {
  doc.references = extractReferencedDocs(doc.markdown, doc.relativeMdPath, docsByRel);
}

for (const doc of docs) {
  doc.referencedBy = docs
    .filter((candidate) => candidate.references.includes(doc.relativeMdPath))
    .map((candidate) => candidate.relativeMdPath);
}

for (const lang of Object.keys(languages)) {
  for (const doc of docs) {
    const targetPath = path.join(outRoot, htmlPathForDoc(doc.relativeMdPath, lang));
    const currentDir = path.posix.dirname(doc.relativeMdPath);

    renderContext = {
      currentDir: currentDir === "." ? "" : currentDir,
      currentMdPath: doc.relativeMdPath,
      docsByRel,
      lang,
    };

    const rendered = renderMarkdown(doc.markdown);
    const html = docShell({
      relativeMdPath: doc.relativeMdPath,
      title: rendered.title || doc.title,
      body: rendered.body,
      headings: rendered.headings,
      docs,
      outgoingDocs: doc.references.map((reference) => docsByRel.get(reference)).filter(Boolean),
      incomingDocs: doc.referencedBy.map((reference) => docsByRel.get(reference)).filter(Boolean),
      lang,
    });

    fs.mkdirSync(path.dirname(targetPath), { recursive: true });
    fs.writeFileSync(targetPath, html);
  }
}

rewritePublishedHtmlLinks();

console.log(`Generated ${markdownFiles.length * Object.keys(languages).length} documentation pages in ${outRoot}`);
