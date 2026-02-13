## GitHub Actions Setup (Automatic Docker Builds)

### Step 1: Push Code to GitHub
```bash
# If not already a git repo
cd online-trail
git init
git add .
git commit -m "Initial Online Trail"

# Create repo on GitHub, then
git remote add origin https://github.com/YOUR_USERNAME/online-trail.git
git push -u origin main
```

### Step 2: Add Docker Hub Secrets

1. Go to your GitHub repo: `https://github.com/YOUR_USERNAME/online-trail`
2. Click **Settings** → **Secrets and variables** → **Actions**
3. Add these secrets:

| Secret Name | Value |
|-------------|-------|
| `DOCKERHUB_USERNAME` | Your Docker Hub username |
| `DOCKERHUB_TOKEN` | Your Docker Hub access token |

**To get Docker Hub token:**
- Go to https://hub.docker.com/settings/security
- Click "New Access Token"
- Give it a name, set permissions to "Read, Write, Delete"
- Copy the token

### Step 3: Trigger Build

Push any commit to main branch:
```bash
git add .
git commit -m "Enable Docker auto-build"
git push origin main
```

### Step 4: Check Build Status

1. Go to **Actions** tab in your GitHub repo
2. You should see the build running
3. Once complete, image will be at: `docker.io/YOUR_USERNAME/online-trail:latest`

---

**Now anyone can run:**
```bash
docker run -d -p 5555:5555 YOUR_USERNAME/online-trail
```
