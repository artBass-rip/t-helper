# GitHub Pages

## Назначение

Репозиторий содержит статическую двуязычную GitHub Pages landing page проекта.
Страница намеренно отделена от продуктового frontend Stage 08: это
документация и презентация репозитория, а не runtime Web UI.

Обе языковые версии используют одну темную framed layout со sticky navigation,
product overview hero, знаком T-Helper, feature strip, позиционированием вокруг
Terraform discovery/security и прямыми входами в документацию. Сгенерированные
страницы документации используют ту же темную визуальную систему со sticky
header, боковым table of contents, связанными документами, backlinks и полным
каталогом документации.

Landing pages обозначают Stage 05 repository manager MVP как закрытый backend
baseline и переводят активный roadmap focus на Stage 06A/06B.

## Файлы

- `docs/index.html` - русская landing page и default entrypoint GitHub Pages.
- `docs/en.html` - английская landing page.
- сгенерированные `*.html` pages в корне артефакта - русская documentation
  shell.
- сгенерированные `en/**/*.html` pages - английская documentation shell.
- `docs/ru/**/*.md` - русские Markdown-источники документации.
- `docs/en/**/*.md` - соответствующие английские Markdown-источники с теми же
  относительными путями.
- `docs/pages.css` - общие responsive styles.
- `docs/pages.js` - общее reveal и hero interaction behavior.
- `docs/.nojekyll` - отключает Jekyll processing для Pages artifact.
- `.github/workflows/pages.yml` - GitHub Actions deployment workflow.

Переключатель языка на landing page использует обычные статические ссылки:

- с русского на английский: `en.html`;
- с английского на русский: `index.html`.

Сгенерированные страницы документации также содержат статический переключатель
RU/EN для того же пути документа. Благодаря этому опубликованный сайт остается
работоспособным даже без JavaScript.

Markdown-документы в `docs/ru/**/*.md` являются каноническими русскими
источниками. Соответствующие документы в `docs/en/**/*.md` являются
каноническими английскими источниками для тех же путей. При публикации
`docs/build-pages.js` использует `docs/ru` как каталог путей, генерирует
русские страницы в корне Pages и английские страницы в `en/`, переписывает
внутренние Markdown-ссылки на сгенерированные HTML-страницы той же языковой
версии и связывает inline-ссылки на документы, например `docs/ru/api.md` или
`docs/en/api.md`, если целевой файл существует. Поэтому опубликованный Pages
site показывает документацию как полноценные стилизованные страницы с общей
навигацией, локальным table of contents, связанными документами, backlinks и
полным каталогом документации. Исходный `.md` той же языковой версии остается
доступен с каждой сгенерированной страницы документа.

## Deployment

Workflow `github-pages` публикует директорию `docs` в ветку `gh-pages` при
push в `master` и при ручном запуске `workflow_dispatch`.

Настройки Pages в репозитории:

- source: `Deploy from a branch`;
- branch: `gh-pages`;
- folder: `/ (root)`.

Такой branch-based deployment не использует GitHub Pages REST API calls из
`actions/configure-pages` и `actions/deploy-pages`. Эти REST calls могут
завершаться ошибкой `Resource not accessible by integration`, если репозиторий
или организация не разрешает `GITHUB_TOKEN` создавать или конфигурировать Pages
site.

Permissions workflow намеренно ограничены:

- `contents: write`.

## Контракт сопровождения

- При обновлении документации или page copy синхронизируйте русские и
  английские Markdown-источники.
- Для обеих языковых версий сохраняйте brand-led hero, product mark и основные
  входы в документацию видимыми в первом viewport.
- Сохраняйте landing page и сгенерированные страницы документации в общей
  темной визуальной системе, если Pages design contract не меняется явно.
- Ссылки внутри Pages artifact должны оставаться относительными к `docs`, если
  целевой файл не должен намеренно открываться на GitHub.
- Сгенерированные страницы документации должны собираться repository-local
  Node.js script без установки packages для публикации Pages.
- Cross-document references должны оставаться относительными Markdown-ссылками
  или inline-ссылками вида `docs/ru/...md` / `docs/en/...md`, чтобы генератор
  Pages мог строить HTML links и backlinks.
- Не трактуйте Pages landing page как Stage 08 Web UI; runtime UI contracts
  остаются в `docs/ru/frontend-ui-contract.md`.
