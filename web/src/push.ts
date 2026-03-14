// Push notification utilities for subscribing/unsubscribing to Web Push.

// Convert a base64url string to a Uint8Array (for applicationServerKey).
function urlBase64ToUint8Array(base64String: string): Uint8Array<ArrayBuffer> {
  const padding = "=".repeat((4 - (base64String.length % 4)) % 4);
  const base64 = (base64String + padding).replace(/-/g, "+").replace(/_/g, "/");
  const rawData = atob(base64);
  const outputArray = new Uint8Array(rawData.length) as Uint8Array<ArrayBuffer>;
  for (let i = 0; i < rawData.length; i++) {
    outputArray[i] = rawData.charCodeAt(i);
  }
  return outputArray;
}

// Register the service worker if not already registered.
export async function registerServiceWorker(): Promise<ServiceWorkerRegistration | null> {
  if (!("serviceWorker" in navigator)) {
    return null;
  }
  try {
    return await navigator.serviceWorker.register("/sw.js");
  } catch {
    return null;
  }
}

// Fetch the VAPID public key from the server.
async function getVAPIDKey(): Promise<string | null> {
  try {
    const res = await fetch("/api/push/vapid-key", { credentials: "include" });
    if (!res.ok) return null;
    const data = await res.json();
    return data.public_key || null;
  } catch {
    return null;
  }
}

// Subscribe to push notifications.
// Returns true if the subscription was saved successfully.
export async function subscribeToPush(): Promise<boolean> {
  const registration = await registerServiceWorker();
  if (!registration) return false;

  const vapidKey = await getVAPIDKey();
  if (!vapidKey) return false;

  try {
    const subscription = await registration.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: urlBase64ToUint8Array(vapidKey),
    });

    const json = subscription.toJSON();
    const res = await fetch("/api/push/subscribe", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        endpoint: json.endpoint,
        keys: {
          p256dh: json.keys?.p256dh || "",
          auth: json.keys?.auth || "",
        },
      }),
    });

    if (!res.ok) {
      // Server failed to persist the subscription — roll back the local subscription
      // so the client and server stay in sync.
      await subscription.unsubscribe();
      return false;
    }
    return true;
  } catch {
    return false;
  }
}

// Unsubscribe from push notifications.
export async function unsubscribeFromPush(): Promise<boolean> {
  const registration = await registerServiceWorker();
  if (!registration) return false;

  try {
    const subscription = await registration.pushManager.getSubscription();
    if (!subscription) return true;

    // Remove from server.
    const res = await fetch("/api/push/subscribe", {
      method: "DELETE",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ endpoint: subscription.endpoint }),
    });

    if (!res.ok) {
      return false;
    }

    // Unsubscribe locally.
    await subscription.unsubscribe();
    return true;
  } catch {
    return false;
  }
}

// Get the active PushSubscription for this browser, or null if none.
// Registers the service worker once and returns the subscription so callers
// can derive multiple values (subscribed status, endpoint) from a single call.
export async function getActivePushSubscription(): Promise<PushSubscription | null> {
  const registration = await registerServiceWorker();
  if (!registration) return null;

  try {
    return await registration.pushManager.getSubscription();
  } catch {
    return null;
  }
}

// Check if the user is currently subscribed to push notifications.
export async function isPushSubscribed(): Promise<boolean> {
  return (await getActivePushSubscription()) !== null;
}

// Check if the browser supports push notifications.
export function isPushSupported(): boolean {
  return "serviceWorker" in navigator && "PushManager" in window;
}

// Get the push endpoint for the current browser subscription, or null if not subscribed.
export async function getCurrentPushEndpoint(): Promise<string | null> {
  const subscription = await getActivePushSubscription();
  return subscription?.endpoint ?? null;
}
