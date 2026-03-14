category: Added
- **Active devices list in notification settings** - Shows all registered push subscriptions with push service hostname, registration date, and "This device" badge. Each device can be individually removed via a dedicated REST endpoint (DELETE /api/push/subscriptions/{id}), with error feedback and local browser subscription cleanup when removing the current device. (Hytte-a6w)
