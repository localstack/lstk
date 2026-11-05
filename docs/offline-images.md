# Custom and offline images

Use `--image` for one start or set `image` in `config.toml` to use a LocalStack image from an internal registry:

```sh
lstk start --image artifactory.example.com/localstack/localstack-enterprise:2026.8.4
```

lstk resolves the custom image before deciding whether LocalStack login or license validation is needed, even when a token is already stored. Missing images are pulled first; moving tags such as `dev` are refreshed using the normal pull policy. If that pull fails and a local copy exists, lstk uses it. A LocalStack Enterprise offline image that bundles its own license starts without a LocalStack token or license check, and lstk does not mount the user’s cached license into it. A stale saved token therefore cannot block its bundled license. An image without a bundled license still requires a LocalStack token, regardless of which registry supplied it.

The registry must be reachable for lstk to pull a missing image. For a fully disconnected start, load or pull the image into the container runtime beforehand. If a private registry requires credentials, use its normal login and pull commands first; lstk's Docker API pull does not currently read Docker CLI registry credentials:

```sh
docker login artifactory.example.com
docker pull artifactory.example.com/localstack/localstack-enterprise:2026.8.4
lstk start --image artifactory.example.com/localstack/localstack-enterprise:2026.8.4
```

Use the same image reference in both commands. A locally present bundled-license image is reused without a pull, including when its tag is `latest`.
