# Adding a Google account

Only needed to add a **Google account**. For a single calendar, the iCal
address is easier: in Google Calendar, **Settings**, pick the calendar, and
copy **Secret address in iCal format** from **Integrate calendar**. Use a
Google account when a calendar has no usable secret address, for example a
work account whose admin turned it off.

CLIcal has no Google login of its own. Each user creates a small OAuth
client in their own Google Cloud project, once. It works the same whether you
run the release binary or build from source.

## One-time setup

1. In the [Google Cloud Console](https://console.cloud.google.com/), create a
   project. Use your personal Google account; the project owner gets no access
   to anyone's calendar.
2. **APIs & Services → Library:** enable **Google Calendar API**.
3. **OAuth consent screen:** choose **External**, fill in an app name and your
   email, and add the scope `.../auth/calendar.readonly` (CLIcal asks for
   nothing else; it can't change your calendars).
4. **Credentials → Create credentials → OAuth client ID:** application type
   **Desktop app**. Copy the client ID (it ends in
   `.apps.googleusercontent.com`) and the client secret.

## Sign in

In CLIcal press `a`, choose Google, and paste the client ID and secret. They
are saved in the encrypted vault once a sign-in succeeds. A browser opens on
Google's sign-in page; pick the account and the calendars to add. Add more
accounts the same way, with the same client.

## Things to know

- **Testing mode:** a new consent screen starts in "Testing". Then every
  account you sign in with must be added under **Test users**, and the
  sign-in **expires after 7 days** (CLIcal marks the account with ⚠; select it and press `s` to sign in again). To
  avoid that, publish the app (**Publish app → In production**). You don't
  need to submit it for verification for your own use.
- **"Google hasn't verified this app":** expected for your own client. Choose
  **Advanced**, then continue.
- **Work accounts:** a company that blocks unconfigured third-party apps will
  refuse the sign-in until an admin allows your client ID in the Admin
  console.
- **Privacy:** the client ID, secret and tokens live only in the vault on your
  machine. Nothing goes to the project owner or to anyone but Google.
