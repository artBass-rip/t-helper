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

function renderInline(value) {
  const codeParts = [];
  let text = escapeHtml(value).replace(/`([^`]+)`/g, (_, code) => {
    const marker = `@@CODE${codeParts.length}@@`;
    codeParts.push(`<code>${code}</code>`);
    return marker;
  });

  text = text.replace(/\[([^\]]+)]\(([^)\s]+)(?:\s+"[^"]*")?\)/g, (_, label, href) => {
    const safeHref = escapeHtml(markdownHrefToHtml(href));
    return `<a href="${safeHref}">${renderInline(label)}</a>`;
  });
  text = text.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
  text = text.replace(/\*([^*]+)\*/g, "<em>$1</em>");

  codeParts.forEach((part, index) => {
    text = text.replace(`@@CODE${index}@@`, part);
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

function docShell({ relativeMdPath, title, body, headings }) {
  const prefix = relativePrefix(relativeMdPath);
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

  return `<!doctype html>
<html lang="ru">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <meta name="description" content="${escapeHtml(title)} - T-Helper documentation.">
    <title>${escapeHtml(title)} - T-Helper Docs</title>
    <link rel="stylesheet" href="${prefix}pages.css">
  </head>
  <body class="doc-page">
    <a class="skip-link" href="#main">К содержанию</a>

    <header class="site-header doc-header" aria-label="Навигация документации">
      <a class="brand" href="${prefix}index.html" aria-label="T-Helper">
        <span class="brand-mark" aria-hidden="true">T</span>
        <span>T-Helper</span>
      </a>
      <nav class="nav-links" aria-label="Разделы документации">
        <a href="${prefix}index.html#docs">Документы</a>
        <a href="${prefix}roadmap.html">Roadmap</a>
        <a href="${prefix}api.html">API</a>
      </nav>
      <div class="lang-switch doc-source">
        <a href="${sourceHref}">.md</a>
      </div>
    </header>

    <main class="doc-shell" id="main">
      <aside class="doc-aside" aria-label="Навигация по документу">
        <a class="doc-home" href="${prefix}index.html">T-Helper Docs</a>
        <span>${escapeHtml(relativeMdPath.split(path.sep).join("/"))}</span>
        <nav class="doc-toc">
          ${toc || '<a href="#main">Начало</a>'}
        </nav>
      </aside>

      <article class="markdown-body">
        ${body}
      </article>
    </main>

    <footer class="site-footer doc-footer">
      <span>T-Helper</span>
      <a href="${prefix}index.html">Русская страница</a>
      <a href="${prefix}en.html">English page</a>
      <a href="${sourceHref}">Исходный Markdown</a>
    </footer>

    <script src="${prefix}pages.js"></script>
  </body>
</html>
`;
}

function rewritePublishedHtmlLinks() {
  for (const htmlPath of findFiles(outRoot, ".html")) {
    const source = fs.readFileSync(htmlPath, "utf8");
    const rewritten = source.replace(
      /(href=["'])([^"']+\.md(?:#[^"']*)?)(["'])/g,
      (_, start, href, end) => `${start}${markdownHrefToHtml(href)}${end}`,
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

for (const mdPath of markdownFiles) {
  const relativeMdPath = path.relative(docsRoot, mdPath);
  const relativeHtmlPath = relativeMdPath.replace(/\.md$/, ".html");
  const targetPath = path.join(outRoot, relativeHtmlPath);
  const markdown = fs.readFileSync(mdPath, "utf8");
  const rendered = renderMarkdown(markdown);
  const html = docShell({
    relativeMdPath,
    title: rendered.title || readableTitle(relativeMdPath),
    body: rendered.body,
    headings: rendered.headings,
  });

  fs.mkdirSync(path.dirname(targetPath), { recursive: true });
  fs.writeFileSync(targetPath, html);
}

rewritePublishedHtmlLinks();

console.log(`Generated ${markdownFiles.length} documentation pages in ${outRoot}`);
