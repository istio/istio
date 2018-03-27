# Generate Json Web Tokens Signed by Google Service Account

The python script (sa-jwt.py) provided here allows the user to generate a JWT signed
by a Google service account.

## Before you start

- Run the following command to install python dependences.
```
  pip install google-auth
```

- Create a service account or use an existing service account, and download the service account private key.

  - In the [Service Accounts page](https://console.cloud.google.com/iam-admin/serviceaccounts),
  click CREATE SERVICE ACCOUNT, or select one of the existing service accounts.
  - Click "Create Key" from the drop-down menu, and select the default JSON key type. The key file
    will automatically downloads to your computer.

## Usage

Type the following command to see the help message.

```
  python sa-jwt.py -h
```

It outputs the following:
```
usage: sa-jwt.py [-h] [-iss ISS] [-aud AUD] [-sub SUB] service_account_file

Python script generates a JWT signed by a Google service account

positional arguments:
  service_account_file  The path to your service account key file (in JSON
                        format).

optional arguments:
  -h, --help            show this help message and exit
  -iss ISS, --iss ISS   iss claim. This should be your service account email.
  -aud AUD, --aud AUD   aud claim
  -sub SUB, --sub SUB   sub claim. If not provided, it is set to the same as
                        iss claim.
```

If you want to add custom claims to the JWT, you can edit sa-jwt.py, and add any claims to JWT payload
(look for "Add any custom claims here" comment in the script).

## Example

Here is an example of using sa-jwt.py to generate a JWT token.
```
  python sa-jwt.py /path/to/service_account.json -iss <YOUR_SERVICE_ACCOUNT_EMAIL> -aud <YOUR_AUDIENCE>
```

