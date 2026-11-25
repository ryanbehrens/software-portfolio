# Shared: app-extras.js

The "Add to home screen" and "Support this project (PayPal)" UI used across my apps, so every app installs and asks for support the same way. It's modeled on Scene Translator.

One file, no dependencies. Each app keeps an identical copy:

| App | Copy | Banner | Cards |
|---|---|---|---|
| Second String | `apps/web/public/app-extras.js` | Home | Bottom of Help |
| CubeLab | `public/app-extras.js` | Top of app | Bottom of Notation |
| Yeas and Nays | `assets/app-extras.js` | Every page | Above every footer |
| Pulse Prep | `assets/js/app-extras.js` | Home | Progress page |
| Turnbot | `public/app-extras.js` | Home | Bottom of Home, plus a game-menu button |
| Firefighter Training | `app-extras.js` | (has its own install button) | Bottom of Home |

## Updating it

1. Edit `app-extras.js` here.
2. Copy it into each app at the path above.
3. Bump the `?v=` number where each app loads it, so browsers fetch the new copy.

## API

```js
AppExtras.init({ name: 'CubeLab', storageKey: 'cubelab-app-extras' });
AppExtras.mountBanner(el);                 // dismissible bar, phones only
AppExtras.mountInstallCard(el, { text });  // status + "Add to home screen"
AppExtras.mountSupport(el, { text });      // PayPal card
AppExtras.install();                       // native prompt, or device-specific steps
```

Colors come from CSS variables each app sets: `--ax-accent`, `--ax-accent-ink`, `--ax-surface`, `--ax-surface-2`, `--ax-text`, `--ax-muted`, `--ax-line`, `--ax-radius`, `--ax-font`, `--ax-modal-bg`.
