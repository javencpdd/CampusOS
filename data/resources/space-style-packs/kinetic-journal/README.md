# Kinetic Journal

An advanced personal homepage example that styles the full `/u/:username` content surface: profile header, avatar, metadata, custom template, synchronized post list, empty state and responsive layout.

The package is owner-bound. When any visitor opens user A's homepage, the API returns A's saved style manifest and the page loads this package from A's profile data.

The sandbox effect declares only `space.profile.read` and `space.posts.read`. CampusStyleSDK pins both capabilities to the route owner and returns sanitized snapshots. The script receives no JWT, DOM, browser storage or arbitrary network access.

Edit `effects/source.ts`, compile it to `effects/main.js`, then run the style-pack validator before applying.
