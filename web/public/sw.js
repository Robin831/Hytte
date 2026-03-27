// Hytte Service Worker — handles push notifications.

self.addEventListener("install", (event) => {
  // Activate immediately without waiting for existing clients to close.
  event.waitUntil(self.skipWaiting());
});

self.addEventListener("activate", (event) => {
  // Claim all clients so the SW is active right away.
  event.waitUntil(self.clients.claim());
});

self.addEventListener("push", (event) => {
  if (!event.data) {
    return;
  }

  let data;
  try {
    data = event.data.json();
  } catch {
    data = { title: "Hytte", body: event.data.text() };
  }

  const title = data.title || "Hytte";
  const options = {
    body: data.body || "",
    icon: data.icon || "/hytte-icon.svg",
    tag: data.tag || undefined,
    data: { url: data.url || "/" },
  };

  event.waitUntil(self.registration.showNotification(title, options));
});

// Validate that a URL is same-origin (or a relative path). Returns "/" on failure.
function safeUrl(raw) {
  if (!raw) return "/";
  try {
    const parsed = new URL(raw, self.location.origin);
    if (parsed.origin !== self.location.origin) return "/";
    return parsed.href;
  } catch {
    return "/";
  }
}

self.addEventListener("notificationclick", (event) => {
  event.notification.close();

  const url = safeUrl(event.notification.data?.url);

  event.waitUntil(
    self.clients.matchAll({ type: "window" }).then((clients) => {
      // Focus an existing window if one is open.
      for (const client of clients) {
        if (client.url.includes(self.location.origin) && "focus" in client) {
          return client.navigate(url).then(() => client.focus());
        }
      }
      // Otherwise open a new window.
      return self.clients.openWindow(url);
    })
  );
});
