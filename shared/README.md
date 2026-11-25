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

## Adding it to a new app

1. **Copy the script.** Put `app-extras.js` in the app's public/static folder and load it with a plain `<script src="/app-extras.js?v=1"></script>` before the app's own code. In Next.js, use `<Script src="/app-extras.js?v=1" strategy="beforeInteractive" />` in the root layout.
2. **Mount it.** Call `AppExtras.init({ name: 'App Name', storageKey: 'app-name-app-extras' })`, then put the banner near the top of the main screen (`mountBanner`) and the install + support cards on the app's "back page" (settings, help, about or progress) with `mountInstallCard` and `mountSupport`. In React, mount inside a `useEffect` on a `ref` and remove the nodes on cleanup (see Second String's `src/components/AppExtras.tsx`).
3. **Theme it.** Set the `--ax-*` CSS variables (below) to the app's own colors.
4. **Make it installable.**
   - `manifest.webmanifest` linked with `<link rel="manifest">`: name, short_name, start_url `/`, display `standalone`, theme and background colors, and icons at 192, 512 and 512 maskable.
   - `<link rel="apple-touch-icon">` (180×180) for iPhone.
   - A service worker (`sw.js`) registered in production. For apps with live data, only handle page loads (see Second String's `public/sw.js`); for apps that work fully in the browser, cache the build for offline use (see CubeLab's).
5. **Make the icons properly.** Render the icon once at **1024×1024** and scale it down (`sips -z 192 192 …`). Do **not** take screenshots in a small browser window: headless Chrome has a minimum window width, so a 180 or 192 px screenshot captures only the top-left corner of the design. The maskable icon needs the artwork inside the middle ~70% because Android crops it to a circle. Add `?v=N` to icon URLs whenever icons change, since phones cache them.
6. **Test** on a phone-sized viewport: the banner should appear, the Install button should show device-specific steps, and the cards should match the app's colors.

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
