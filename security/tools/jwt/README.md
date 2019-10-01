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

```bash
python sa-jwt.py -h
```

It outputs the following:

```plain
usage: ./sa-jwt.py [-h] [-iss ISS] [-aud AUD] [-sub SUB] [-claims CLAIMS] service_account_file

Python script generates a JWT signed by a Google service account

positional arguments:
  service_account_file  The path to your service account key file (in JSON
                        format).

optional arguments:
  -h, --help            show this help message and exit
  -iss ISS, --iss ISS   iss claim. This should be your service account email.
  -aud AUD, --aud AUD   aud claim. This is comma-separated-list of audiences.
  -sub SUB, --sub SUB   sub claim. If not provided, it is set to the same as
                        iss claim.
  -claims CLAIMS, --claims CLAIMS
                        Other claims in format name1:value1,name2:value2 etc.
                        Only string values are supported.
```

## Example

Here is an example of using sa-jwt.py to generate a JWT token.

```bash
./sa-jwt.py /path/to/service_account.json -iss frod@gserviceaccount.com -aud foo,bar
./sa-jwt.py /path/to/service_account.json -iss frod@gserviceaccount.com -aud foo,bar -claims key1:value1,key2:value2
```
