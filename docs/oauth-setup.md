# OAuth Setup

PushPixel needs OAuth 2.0 credentials to access your Google Photos library.
These steps are done once per user.

## 1. Create a project

1. Go to [console.cloud.google.com](https://console.cloud.google.com)
2. Click the project dropdown (top-left) → **New Project**
3. Name it `PushPixel` → **Create**

## 2. Enable the API

1. Go to **APIs & Services → Library**
2. Search for **Google Photos Library API**
3. Click it → **Enable**

## 3. Register your app

1. Go to **https://console.cloud.google.com/auth/audience** (Auth Platform → Audience)
2. If prompted, select your PushPixel project
3. User Type: **External** → **Create**
4. Fill in required fields (app name, emails) → **Save and Continue**
5. **Publish your app** so refresh tokens don't expire in 7 days:

   Scroll to the **Publishing status** section at the bottom of the page → click **PUBLISH APP** → confirm.

   > This does **not** require full app verification. The Photos Library API scopes show a
   > one-time "unverified app" warning to users, but tokens are long-lived in "In production"
   > mode. In "Testing" mode, Google issues refresh tokens that expire after 7 days.

6. **Add yourself as a test user** (required while the app is unverified):

   Under **Test users** → **+ Add Users** → enter your Gmail → **Save**.

   Without this step, Google shows an "unverified app" error and you can't sign in.

## 4. Set up branding

1. Go to **https://console.developers.google.com/auth/branding** (Auth Platform → Branding)
2. Enter your **App name** (`PushPixel`), **User support email**, and **Developer contact email**

## 5. Create credentials

1. Go to **https://console.developers.google.com/auth/clients** (Auth Platform → Clients)
2. Click **CREATE CLIENT**
3. Application type: **Desktop application**
4. Name: `PushPixel Desktop`
5. Click **Create**
6. A popup shows your **Client ID** and **Client Secret** — copy both into `config.yaml`

> Desktop application clients do not need redirect URIs — Google auto-allows `http://localhost` on any port.

## 6. Add to config

Paste the values into `config.yaml`:

```yaml
auth:
  client_id: "xxxxx.apps.googleusercontent.com"
  client_secret: "GOCSPX-xxxxx"
  token_dir: "."
```

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| "unverified app" at sign-in | Make sure you added your email as a test user under Auth Platform → Audience |
| "access_denied" | Check you enabled the Google Photos Library API |
| "token not found" after restart | The token file stores your session — keep it safe |
| Token expires after 7 days | Your app is still in "Testing" mode. Go to **Auth Platform → Audience**, scroll to **Publishing status**, and click **PUBLISH APP**. Then delete your token file and reconnect. |
| Redirect went to `localhost` on a remote server | PushPixel uses Desktop OAuth for simplicity, which only auto-approves localhost. Replace `localhost:1978` in the URL bar with your server's hostname — keep `/oauth/callback?...` and all query parameters unchanged. |
