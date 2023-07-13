import sys
import argparse
import time
from azure.identity import DefaultAzureCredential
from azure.keyvault.secrets import SecretClient
from kubernetes import client, config
from azure.core.exceptions import AzureError, HttpResponseError
from typing import Optional
import subprocess
import os
from typing import Tuple
import json
import shutil
from pathlib import Path

def uninstall_chart(release_name:str = "csi") -> None:
    print(f"Uninstalling the {release_name}...")
    try:
        result = subprocess.run(
            [
                "helm",
                "uninstall",
                f"{release_name}",
                "-n",
                "kube-system"
            ],
            check=True,
            stderr=subprocess.PIPE,
            stdout=subprocess.PIPE,
            text=True
        )
        print("Uninstalled the default CSI driver and provider successfully: \n" + result.stdout)
    except subprocess.CalledProcessError as e:
        raise RuntimeError("Failed to list helm charts installed in the kube-system namespace:" + str(e.stderr)) from e

def add_helm_repo(helm_chart_name: str, helm_chart_url: str) -> None:
    try:
        subprocess.run(["helm", "repo", "add", helm_chart_name, helm_chart_url], check=True)
        print(f"Helm repository for {helm_chart_name} from {helm_chart_url} added successfully.")
    except subprocess.CalledProcessError as e:
        raise RuntimeError(f"Failed to add Helm {helm_chart_name} repository for {helm_chart_url}.") from e

def check_tool_installed(*tool_names: str) -> None:
    for tool_name in tool_names:
        if shutil.which(tool_name) is not None:
            print(f"{tool_name} is installed.")
        else:
            print(f"{tool_name} is not installed.")
            sys.exit(1)

def generate_key_pair() -> Tuple[str, str]:
    try:
        key_dir = os.path.abspath(os.path.dirname(__file__))
        key_path = os.path.join(key_dir, 'sa.key')
        pub_path = os.path.join(key_dir, 'sa.pub')
        subprocess.run(['openssl', 'genrsa', '-out', key_path, '2048'], check=True)
        subprocess.run(['openssl', 'rsa', '-in', key_path, '-pubout', '-out', pub_path], check=True)
        print("Public and private key pair generated successfully.")
        return key_path, pub_path
    except subprocess.CalledProcessError as ex:
        print(f"Failed to generate key pair: {ex}")
        sys.exit(1)

def get_storage_connection_string(storage_account: str) -> str:
    try:
        result = subprocess.run(
            ['az', 'storage', 'account', 'show-connection-string', '-n', storage_account, '--output', 'json'],
            capture_output=True,
            text=True,
            check=True
        )
        connection_string = result.stdout.strip()
        connection_string = json.loads(connection_string)["connectionString"]
        connection_string = "\"" + str(connection_string) + "\""
        return connection_string
    except subprocess.CalledProcessError as ex:
        print(f"Failed to retrieve storage account connection string: {ex}")
        sys.exit(1)

def get_user_assigned_identity_info(identity_name: str, resource_group: str) -> Tuple[str, str]:
    try:
        result = subprocess.run(['az', 'identity', 'show', '-n', identity_name, "--resource-group", resource_group, '--output', 'json'], capture_output=True, text=True, check=True)
        identity_info = json.loads(result.stdout)
        client_id = identity_info["clientId"]
        object_id = identity_info["principalId"]
        return client_id, object_id
    except subprocess.CalledProcessError as ex:
        raise RuntimeError(f"Failed to retrieve user-assigned identity information: {ex.stderr}") from ex
        sys.exit(1)

def upload_configs_configurations(
    storage_account: str,
    storage_container: str,
    public_key_path: str,
    connection_string: str
) -> None:
    blob_name = ".well-known/openid-configuration"
    file_path = "openid-configuration.json"
    jwks_output_file = "jwks.json"

    try:
        create_openid_configuration_file(storage_account=storage_account,storage_container=storage_container)
        
        # Upload openid-configuration.json to Azure Storage
        upload_command = f'az storage blob upload --connection-string {connection_string} --account-name {storage_account} --container-name {storage_container} --name {blob_name} --type block --overwrite --file {file_path}'
        subprocess.run(upload_command, shell=True, check=True)
        print("openid-configuration.json uploaded to Azure Storage successfully.")

        # Run azwi jwks command to generate jwks.json
        jwks_command = f'azwi jwks --public-keys {public_key_path} --output-file {jwks_output_file}'
        subprocess.run(jwks_command, shell=True, check=True)
        print("jwks.json file generated successfully.")

        # Upload jwks.json to Azure Storage
        upload_jwks_command = f'az storage blob upload --connection-string {connection_string} --account-name {storage_account} --container-name {storage_container} --name openid/v1/jwks --type block --overwrite --file {jwks_output_file}'
        subprocess.run(upload_jwks_command, shell=True, check=True)
        print("jwks.json uploaded to Azure Storage successfully.")

        # Run the curl command to retrieve the jwks_uri
        curl_command = f'curl -s "https://{storage_account}.blob.core.windows.net/{storage_container}/openid/v1/jwks"'
        subprocess.run(curl_command, shell=True, check=True)

    except subprocess.CalledProcessError as ex:
        print(f"Failed to upload configs to Azure Storage: {str(ex)}")
        sys.exit(1)

def create_openid_configuration_file(storage_account: str, storage_container: str) -> None:
    config_data = {
        "issuer": f"https://{storage_account}.blob.core.windows.net/{storage_container}/",
        "jwks_uri": f"https://{storage_account}.blob.core.windows.net/{storage_container}/openid/v1/jwks",
        "response_types_supported": ["id_token"],
        "subject_types_supported": ["public"],
        "id_token_signing_alg_values_supported": ["RS256"]
    }

    file_path = "openid-configuration.json"

    try:
        with open(file_path, "w") as file:
            json.dump(config_data, file, indent=2)
        print("openid-configuration.json file created successfully.")
    except Exception as ex:
        print(f"Failed to create openid-configuration.json file: {str(ex)}")
        sys.exit(1)

def get_keyvault_url(resource_group: str, keyvault_name: str) -> str:
    try:
        result = subprocess.run(['az', 'keyvault', 'show', '--name', keyvault_name, '--resource-group', resource_group, '--query', 'properties.vaultUri', '--output', 'json'], capture_output=True, text=True, check=True)
        keyvault_url = result.stdout.strip().strip('"')
        return keyvault_url
    except subprocess.CalledProcessError as ex:
        print(f"Failed to retrieve Key Vault URL: {ex}")
        sys.exit(1)

def create_kind_cluster(cluster_name: str = "kind") -> None:
    service_account_key_file = os.environ.get("SERVICE_ACCOUNT_KEY_FILE")
    service_account_signing_key_file = os.environ.get("SERVICE_ACCOUNT_SIGNING_KEY_FILE")
    service_account_issuer = os.environ.get("SERVICE_ACCOUNT_ISSUER")

    if service_account_key_file is None or service_account_signing_key_file is None or service_account_issuer is None:
        print("ERROR: Required environment variables are not set.")
        return

    yaml_config = f"""kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  extraPortMappings:
  - containerPort: 30123
    hostPort: 30123
    protocol: TCP
  extraMounts:
  - hostPath: {service_account_key_file}
    containerPath: /etc/kubernetes/pki/sa.pub
  - hostPath: {service_account_signing_key_file}
    containerPath: /etc/kubernetes/pki/sa.key
  kubeadmConfigPatches:
  - |
    kind: ClusterConfiguration
    apiServer:
      extraArgs:
        service-account-issuer: {service_account_issuer}
        service-account-key-file: /etc/kubernetes/pki/sa.pub
        service-account-signing-key-file: /etc/kubernetes/pki/sa.key
    controllerManager:
      extraArgs:
        service-account-private-key-file: /etc/kubernetes/pki/sa.key
  image: kindest/node:v1.27.1@sha256:b7d12ed662b873bd8510879c1846e87c7e676a79fefc93e17b2a52989d3ff42b
"""

    config_file = "cluster-config.yaml"

    try:
        with open(config_file, "w") as file:
            file.write(yaml_config)

        print("\nCluster configuration YAML:")
        print(yaml_config)
        try:
            # Use the YAML file to create the cluster using `kind`
            result = subprocess.run(["kind", "create", "cluster", "--name", cluster_name, "--config", config_file], text=True, check=True)
            if result.returncode == 0:
                print("Kind cluster created successfully.")
            else:
                print(f"Failed to create cluster. Return code: {result.returncode}")
        except subprocess.CalledProcessError as e:
            raise RuntimeError(f"command {e.cmd} return with error (code {e.returncode}): {e.stderr}")

        # Remove the YAML file
        os.remove(config_file)
        print("\nCluster configuration YAML file removed.")

    except Exception as ex:
        print(f"Failed to create cluster: {str(ex)}")
        sys.exit(1)

def delete_kind_cluster(cluster_name: str = "kind") -> None:
    try:
        subprocess.run(["kind", "delete", "cluster", "--name", cluster_name], check=True)
        print("Kind cluster deleted successfully.")
    except subprocess.CalledProcessError as ex:
        print(f"Failed to delete cluster: {ex}")
        sys.exit(1)

def unset_environment_variables() -> None:
    """
    Unexport environment variables for the specified values. The script must be invoked 
    eval `python3 pyconfig_script.py --usev` to unset the environment variables in the current shell.
    """
    unset_workload_identity_env_vars()
    os.unsetenv("SUBSCRIPTION_ID")
    os.unsetenv("RESOURCE_GROUP")
    os.unsetenv("KEYVAULT_NAME")
    os.unsetenv("KEYVAULT_CSI_SECRET_NAME")
    os.unsetenv("MANAGED_IDENTITY")
    os.unsetenv("AZURE_STORAGE_ACCOUNT")
    os.unsetenv("AZURE_STORAGE_CONTAINER")
    os.unsetenv("AZURE_STORAGE_CONNECTION_STRING")
    os.unsetenv("SERVICE_ACCOUNT_ISSUER")
    os.unsetenv("SERVICE_ACCOUNT_KEY_FILE")
    os.unsetenv("SERVICE_ACCOUNT_SIGNING_KEY_FILE")
    os.unsetenv("MANAGED_IDENTITY_CLIENT_ID")
    os.unsetenv("MANAGED_IDENTITY_OBJECT_ID")
    os.unsetenv("KEYVAULT_URL")
    os.unsetenv("CLUSTER_NAME")
    os.unsetenv("AZURE_TENANT_ID")
    os.unsetenv("HOST_PATH_TO_MAP")

    export_commands = [
        f'unset SUBSCRIPTION_ID',
        f'unset RESOURCE_GROUP',
        f'unset KEYVAULT_NAME',
        f'unset KEYVAULT_CSI_SECRET_NAME',
        f'unset MANAGED_IDENTITY',
        f'unset AZURE_STORAGE_ACCOUNT',
        f'unset AZURE_STORAGE_CONTAINER',
        f'unset AZURE_STORAGE_CONNECTION_STRING',
        f'unset SERVICE_ACCOUNT_ISSUER',
        f'unset SERVICE_ACCOUNT_KEY_FILE',
        f'unset SERVICE_ACCOUNT_SIGNING_KEY_FILE',
        f'unset MANAGED_IDENTITY_CLIENT_ID',
        f'unset MANAGED_IDENTITY_OBJECT_ID',
        f'unset KEYVAULT_URL',
        f'unset AZURE_TENANT_ID',
        f'unset HOST_PATH_TO_MAP',
        f'unset CLUSTER_NAME'
    ]

    for command in export_commands:
        print(command)
    
    

def export_environment_variables(
    keyvault_name: str,
    managed_identity_name: str,
    storage_account: str,
    storage_container: str,
    service_account_issuer: str,
    subscription_id: str,
    service_account_key_file: str,
    service_account_signing_key_file: str,
    cluster_name: str,
    tenant_id: str,
    resource_group: Optional[str] = "default-test-rg",
    secret_name: Optional[str] = "default-test-secret",
    export_to_shell: bool = False
) -> None:
    os.environ["SUBSCRIPTION_ID"] = subscription_id
    os.environ["RESOURCE_GROUP"] = resource_group
    os.environ["KEYVAULT_NAME"] = keyvault_name
    os.environ["KEYVAULT_CSI_SECRET_NAME"] = secret_name
    os.environ["MANAGED_IDENTITY"] = managed_identity_name
    os.environ["AZURE_STORAGE_ACCOUNT"] = storage_account
    os.environ["AZURE_STORAGE_CONTAINER"] = storage_container
    
    os.environ["SERVICE_ACCOUNT_ISSUER"] = service_account_issuer
    os.environ["SERVICE_ACCOUNT_KEY_FILE"] = service_account_key_file
    os.environ["SERVICE_ACCOUNT_SIGNING_KEY_FILE"] = service_account_signing_key_file
    
    # Compute user-assigned identity client ID and object ID
    client_id, object_id = get_user_assigned_identity_info(identity_name=managed_identity_name, resource_group=resource_group)
    os.environ["MANAGED_IDENTITY_CLIENT_ID"] = client_id
    os.environ["MANAGED_IDENTITY_OBJECT_ID"] = object_id
    keyvault_url = get_keyvault_url(resource_group, keyvault_name)
    os.environ["KEYVAULT_URL"] = keyvault_url
    os.environ["CLUSTER_NAME"] = cluster_name
    os.environ["AZURE_TENANT_ID"] = tenant_id
    storage_connection_string =  get_storage_connection_string(storage_account)
    os.environ["AZURE_STORAGE_CONNECTION_STRING"] = storage_connection_string
    
    if export_to_shell:
        export_commands = [
            f'export SUBSCRIPTION_ID="{subscription_id}"',
            f'export RESOURCE_GROUP="{resource_group}"',
            f'export KEYVAULT_NAME="{keyvault_name}"',
            f'export KEYVAULT_CSI_SECRET_NAME="{secret_name}"',
            f'export MANAGED_IDENTITY="{managed_identity_name}"',
            f'export AZURE_STORAGE_ACCOUNT="{storage_account}"',
            f'export AZURE_STORAGE_CONTAINER="{storage_container}"',
            f'export AZURE_STORAGE_CONNECTION_STRING={storage_connection_string}',
            f'export SERVICE_ACCOUNT_ISSUER="{service_account_issuer}"',
            f'export SERVICE_ACCOUNT_KEY_FILE="{service_account_key_file}"',
            f'export SERVICE_ACCOUNT_SIGNING_KEY_FILE="{service_account_signing_key_file}"',
            f'export MANAGED_IDENTITY_CLIENT_ID="{client_id}"',
            f'export MANAGED_IDENTITY_OBJECT_ID="{object_id}"',
            f'export KEYVAULT_URL="{keyvault_url}"',
            f'export AZURE_TENANT_ID="{tenant_id}"',
            f'export CLUSTER_NAME="{cluster_name}"'
        ]

        for command in export_commands:
            print(command)
        


def setup_kubernetes_with_keyvault(
    subscription_id: str = 'MySubscription',
    keyvault_name: str = 'MyKeyVault',
    managed_identity_name: str = 'MyManagedIdentity',
    storage_account: str = 'MyStorageAccount',
    storage_container: str = 'MyStorageAccountContainer',
    service_account_issuer: str = 'MyServiceAccountIssuer',
    cluster_name: str = 'kind',
    secret_name: Optional[str] = 'default-test-secret',
    resource_group: Optional[str] = 'default-test-rg',
    tenant_id: Optional[str] ='default-test-tenant-id'
) -> None:
    """
    Configure a Kubernetes cluster with Azure Key Vault integration using a user-assigned managed identity.

    Args:
        subscription_id (str, optional): Azure subscription ID.
        keyvault_name (str, optional): Azure Key Vault name.
        
    """
    # Set the Azure subscription
    try:
        subprocess.run(['az', 'account', 'set', '-s', subscription_id], check=True, stderr=subprocess.PIPE, stdout=subprocess.PIPE, text=True)
    except subprocess.CalledProcessError as ex:
        print(f"Failed to set Azure subscription: {ex}")
        sys.exit(1)

    # Retrieve the Azure tenant ID
    try:
        result = subprocess.run(
            ['az', 'account', 'show', '-s', subscription_id, '--query', 'tenantId', '-otsv'],
            capture_output=True,
            text=True,
            check=True
        )
        tenant_id = result.stdout.strip()
    except subprocess.CalledProcessError as ex:
        print(f"Failed to retrieve Azure tenant ID: {ex}")
        sys.exit(1)
    
    # Generate the key pair
    service_account_signing_key_file, service_account_key_file = generate_key_pair()
    print(f"Generated key pair: {service_account_key_file}, {service_account_signing_key_file}")    
    
    try:
        # Retrieve the Azure credentials using the managed identity
        try:
            credential = DefaultAzureCredential(managed_identity_client_id=managed_identity_name)
        except AzureError as ex:
            print(f"Failed to retrieve Azure credentials: {str(ex)}")
            sys.exit(1)
        
        # Retrieve the Azure Key Vault URL
        keyvault_url = f"https://{keyvault_name}.vault.azure.net"

        # Create a client for interacting with Azure Key Vault
        try:
            secret_client = SecretClient(vault_url=keyvault_url, credential=credential)
        except AzureError as ex:
            print(f"Failed to create Azure Key Vault client: {str(ex)}")
            sys.exit(1)

        # Use the secret client as needed
        try:
            # Example: List all secrets in the Key Vault
            print("Listing secrets in Key Vault...")
            secrets = secret_client.list_properties_of_secrets()
            for secret in secrets:
                print(f"Secret Name: {secret.name}")
        except (AzureError, HttpResponseError) as ex:
            print(f"An Azure Key Vault error occurred: {str(ex)}")
            sys.exit(1)

    except AzureError as ex:
        print(f"An Azure error occurred: {str(ex)}")
        sys.exit(1)
    except Exception as ex:
        print(f"An error occurred: {str(ex)}")
        sys.exit(1)

    # Upload the public key to Azure Storage if storage account and container are provided
    if storage_account and storage_container:
        try:
            from azure.storage.blob import BlobServiceClient

            # Retrieve the Azure Storage account connection string
            storage_connection_string = get_storage_connection_string(storage_account)

            upload_configs_configurations(storage_container=storage_container, 
                                          storage_account=storage_account, 
                                          public_key_path='sa.pub',
                                          connection_string=storage_connection_string)
            export_environment_variables(
                resource_group=resource_group,
                keyvault_name=keyvault_name,
                secret_name=secret_name,
                managed_identity_name=managed_identity_name,
                storage_account=storage_account,
                storage_container=storage_container,
                service_account_issuer=service_account_issuer,
                subscription_id=subscription_id,
                service_account_key_file=service_account_key_file,
                service_account_signing_key_file=service_account_signing_key_file,
                tenant_id=tenant_id,
                cluster_name=cluster_name
            )
        except Exception as ex:
            print(f"Failed to handle Azure Storage: {str(ex)}")
            sys.exit(1)

def set_workload_identity_env_vars(service_account_wi:str, service_account_wi_namespace: str, export_to_shell:bool=False):
    os.environ["SERVICE_ACCOUNT_WI_NAME"] = service_account_wi
    os.environ["SERVICE_ACCOUNT_WI_NAMESPACE"] = service_account_wi_namespace
    if export_to_shell:
        export_commands = [
            f'export SERVICE_ACCOUNT_WI_NAME={service_account_wi}',
            f'export SERVICE_ACCOUNT_WI_NAMESPACE={service_account_wi_namespace}'
        ]
        for command in export_commands:
            print(command)

def unset_workload_identity_env_vars():
    os.unsetenv("SERVICE_ACCOUNT_WI_NAME")
    os.unsetenv("SERVICE_ACCOUNT_WI_NAMESPACE")

    export_commands = [
        f'unset SERVICE_ACCOUNT_WI_NAME',
        f'unset SERVICE_ACCOUNT_WI_NAMESPACE'
    ]
    for command in export_commands:
        print(command)   

def install_workload_identity_webhook():
    azure_tenant_id = os.environ.get("AZURE_TENANT_ID")
    add_helm_repo(helm_chart_name="azure-workload-identity", helm_chart_url="https://azure.github.io/azure-workload-identity/charts")
    try:
        subprocess.run(
            [
                "helm",
                "install",
                "workload-identity-webhook",
                "azure-workload-identity/workload-identity-webhook",
                "--namespace",
                "azure-workload-identity-system",
                "--create-namespace",
                "--set",
                f"azureTenantID={azure_tenant_id}"
            ],
            check=True,
            stderr=subprocess.PIPE,
            stdout=subprocess.PIPE,
            text=True
        )
        print("Workload identity webhook installed successfully.")
    except subprocess.CalledProcessError as ex:
        print(f"Failed to install workload identity webhook: {ex}")
        sys.exit(1)

def setup_webhook_workload_identity() -> None:
    application_client_id = os.environ.get("APPLICATION_CLIENT_ID")
    user_assigned_identity_client_id = os.environ.get("MANAGED_IDENTITY_CLIENT_ID")
    azure_tenant_id = os.environ.get("AZURE_TENANT_ID")
    user_assigned_managed_identity = os.environ.get("MANAGED_IDENTITY")
    resource_group = os.environ.get("RESOURCE_GROUP")
    service_account_issuer = os.environ.get("SERVICE_ACCOUNT_ISSUER")
    service_account_wi_name = os.environ.get("SERVICE_ACCOUNT_WI_NAME")
    service_account_wi_namespace = os.environ.get("SERVICE_ACCOUNT_WI_NAMESPACE")

    # Create the ServiceAccount with annotations
    service_account_yaml = f"""\
apiVersion: v1
kind: ServiceAccount
metadata:
  annotations:
    azure.workload.identity/client-id: {application_client_id or user_assigned_identity_client_id}
  labels:
    azure.workload.identity/use: "true"
  name: {service_account_wi_name}
  namespace: {service_account_wi_namespace}
"""

    # Apply the ServiceAccount
    try:
        subprocess.run(["kubectl", "apply", "-f", "-"], input=service_account_yaml, check=True, stderr=subprocess.PIPE, stdout=subprocess.PIPE, text=True)
    except subprocess.CalledProcessError as e:
        raise RuntimeError("Failed to apply the ServiceAccount.") from e

    # Annotate the ServiceAccount with tenant ID
    try:
        subprocess.run(
            [
                "kubectl",
                "annotate",
                "sa",
                service_account_wi_name,
                "-n",
                service_account_wi_namespace,
                f"azure.workload.identity/tenant-id={azure_tenant_id}"
            ],
            check=True,
            stderr=subprocess.PIPE,
            stdout=subprocess.PIPE,
            text=True
        )
    except subprocess.CalledProcessError as e:
        raise RuntimeError("Failed to annotate the ServiceAccount with the tenant ID.") from e

    # Create federated credential
    try:
        subprocess.run(
            [
                "az",
                "identity",
                "federated-credential",
                "create",
                "--name",
                "kubernetes-federated-credential-csi",
                "--identity-name",
                user_assigned_managed_identity,
                "--resource-group",
                resource_group,
                "--issuer",
                service_account_issuer,
                "--subject",
                f"system:serviceaccount:{service_account_wi_namespace}:{service_account_wi_name}"
            ],
            check=True,
            stderr=subprocess.PIPE,
            stdout=subprocess.PIPE,
            text=True
        )
    except subprocess.CalledProcessError as e:
        raise RuntimeError("Failed to create the federated credential.") from e

def force_delete_pod(pod_name:str) -> None:
    print(f"Deleting the {pod_name} testing Pod...")
    try:
        result = subprocess.run(["kubectl", "delete", "pod", f"{pod_name}", "--force"], check=True, capture_output=True, text=True)
        print(f"Deleted the {pod_name} testing Pod: \n{result.stdout}")
    except subprocess.CalledProcessError as e:
        # TODO: this should be moved to a generic function (there are at least 2 places where we do this)
        error_message = str(e.stderr)
        error_message_lower = error_message.lower()
        if "notfound" in error_message_lower and f"{pod_name}".lower() in error_message_lower:
             print(f"Pod doesn't exist. Error is: {error_message}")
        else:
            raise RuntimeError(f"Failed to delete the testing Pod.\n{e.stderr}") from e


def retrieve_logs_for_pod(pod_name:str) -> None:
    try:
        result = subprocess.run(
            ["kubectl", "logs", f"{pod_name}"],
            text=True,
            check=True,
            stderr=subprocess.PIPE,
            stdout=subprocess.PIPE
        )
        print(f"Retrieved logs for {pod_name} pod: \n{result.stdout}")
    except subprocess.CalledProcessError as e:
        raise RuntimeError("Failed to retrieve logs for the testing Pod:" + str(e.stderr)) from e


def test_webhook_integration() -> None:
    print("Testing webhook integration...")
    service_account_wi_namespace = os.environ.get("SERVICE_ACCOUNT_WI_NAMESPACE")
    service_account_wi_name = os.environ.get("SERVICE_ACCOUNT_WI_NAME")
    keyvault_url = os.environ.get("KEYVAULT_URL")
    keyvault_csi_secret_name = os.environ.get("KEYVAULT_CSI_SECRET_NAME")

    # Create the Pod with the required configurations
    pod_yaml = f"""\
apiVersion: v1
kind: Pod
metadata:
  name: quick-start-wi-webhook-csi-secret
  namespace: {service_account_wi_namespace}
  labels:
    azure.workload.identity/use: "true"
spec:
  serviceAccountName: {service_account_wi_name}
  containers:
    - image: ghcr.io/azure/azure-workload-identity/msal-go
      name: oidc
      env:
      - name: KEYVAULT_URL
        value: {keyvault_url}
      - name: SECRET_NAME
        value: {keyvault_csi_secret_name}
  nodeSelector:
    kubernetes.io/os: linux
"""
    retry_count = 0
    max_retries = 5
    wait_time = 60

    while retry_count < max_retries:
        try:
            print("Creating the testing Pod - attempt:", {retry_count + 1})
            subprocess.run(
                [
                    "kubectl",
                    "apply",
                    "-f",
                    "-",
                ],
                input=pod_yaml,
                stderr=subprocess.PIPE,
                stdout=subprocess.PIPE,
                text=True,
                check=True,
            )
        except subprocess.CalledProcessError as e:
            error_message = str(e.stderr) 
            error_message_lower = error_message.lower()
            print(f"Federation isn't ready yet - please wait.\nError is: {error_message}")

            if "internalerror" in error_message_lower and "connection refused" in error_message_lower:
                if retry_count < max_retries:
                    print(f"Retrying after {wait_time} seconds...")
                    time.sleep(wait_time)
                    retry_count += 1
                    continue
                elif retry_count >= max_retries:
                    raise RuntimeError("Exceeded maximum retry attempts. Failed to create the testing Pod.")
            else:
                raise RuntimeError("Failed to create the testing Pod.") from e
        break

    try:
        result = subprocess.run(
            ["kubectl", "describe", "pod", "quick-start-wi-webhook-csi-secret"],
            stderr=subprocess.PIPE,
            stdout=subprocess.PIPE,
            text=True,
            check=True,
        )
        print(f"Created webhook test Pod successfully:\n{result.stdout}")
    except subprocess.CalledProcessError as e:
        raise RuntimeError("Failed to describe the testing Pod:" + str(e.stderr)) from e
    
    #TODO: a retry loop here would be nice until we can get the logs with the correct information:
    # the secret we expect to be mounted in the pod - and check it's what we expected
    print("Waiting for the quick-start-wi-webhook-csi-secret Pod to be ready to retrive the logs...")
    time.sleep(15)
    try:
        retrieve_logs_for_pod("quick-start-wi-webhook-csi-secret")
    except subprocess.CalledProcessError as e:
        raise RuntimeError("Failed to retrieve logs for the testing Pod:" + str(e.stderr)) from e

def create_secret_provider_class() -> None:
    print("Creating the Test SecretProviderClass...")
    keyvault_secret_name = os.environ.get("KEYVAULT_CSI_SECRET_NAME")
    managed_identity_client_id = os.environ.get("MANAGED_IDENTITY_CLIENT_ID")
    keyvault_name = os.environ.get("KEYVAULT_NAME")
    tenant_id = os.environ.get("AZURE_TENANT_ID")

    # Create the SecretProviderClass using the kubectl command
    secret_provider_class_yaml = f"""\
apiVersion: secrets-store.csi.x-k8s.io/v1
kind: SecretProviderClass
metadata:
  name: secretstoreproviderclass-test-akv1
spec:
  provider: azure
  secretObjects:
  - secretName: {keyvault_secret_name}
    type: Opaque
    labels:
      environment: "test"
    data:
    - objectName: my-csi-secret-test-local
      key: my-csi-secret-test-local-key
  parameters:
    usePodIdentity: "false"
    clientID: {managed_identity_client_id}
    keyvaultName: {keyvault_name}
    objects: |
      array:
        - |
          objectName: {keyvault_secret_name}
          objectType: secret
          objectAlias: my-csi-secret-test-local
          objectVersion:
    tenantId: {tenant_id}
"""

    try:
        subprocess.run(["kubectl", "apply", "-f", "-"], input=secret_provider_class_yaml.encode(), check=True)
        print("SecretProviderClass created successfully.")
    except subprocess.CalledProcessError as e:
        raise RuntimeError("Failed to create the SecretProviderClass.") from e
    
    try:
        subprocess.run(["kubectl", "get", "secretproviderclass", "secretstoreproviderclass-test-akv1"], check=True)
        print("SecretProviderClass retrieved successfully.")
    except subprocess.CalledProcessError as e:
        raise RuntimeError("Failed to retrieve the SecretProviderClass.") from e

def create_pod_with_secret_provider_class() -> None:
    keyvault_secret_name = os.environ.get("KEYVAULT_CSI_SECRET_NAME")
    service_account_wi_name = os.environ.get("SERVICE_ACCOUNT_WI_NAME")

    pod_yaml = f"""\
kind: Pod
apiVersion: v1
metadata:
  name: test-csi-driver-installation-pod
spec:
  serviceAccount: {service_account_wi_name}
  containers:
  - image: registry.k8s.io/e2e-test-images/busybox:1.29-4
    name: busybox
    imagePullPolicy: IfNotPresent
    command:
    - "/bin/sleep"
    - "10000"
    volumeMounts:
    - name: {keyvault_secret_name}
      mountPath: "/mnt/secrets-store"
      readOnly: true
  volumes:
    - name: {keyvault_secret_name}
      csi:
        driver: secrets-store.csi.k8s.io
        readOnly: true
        volumeAttributes:
          secretProviderClass: "secretstoreproviderclass-test-akv1"
"""

    try:
        result = subprocess.run(["kubectl", "apply", "-f", "-"], input=pod_yaml.encode(), check=True)
        print(f"Pod created successfully. {result.stdout}")
    except subprocess.CalledProcessError as e:
        raise RuntimeError(f"Failed to create the Pod.: {result.stderr}") from e
    
    #TODO: a retry loop here would be nice until we can get the logs with the correct information:
    # the secret we expect to be mounted in the pod - and check it's what we expected
    print("Waiting for the test-csi-driver-installation-pod Pod to be ready to retrive the logs...")
    time.sleep(30)
    retrieve_logs_for_pod("test-csi-driver-installation-pod")

def install_default_csi_driver_and_provider_bundle() -> None:
    print("Installing the default CSI driver and provider...")
    add_helm_repo(helm_chart_name="csi-secrets-store-provider-azure", helm_chart_url="https://azure.github.io/secrets-store-csi-driver-provider-azure/charts")
    
    try:
        result = subprocess.run(
            [
                "helm",
                "install",
                "default-csi-driver-and-provider-bundle",
                "csi-secrets-store-provider-azure/csi-secrets-store-provider-azure",
                "--namespace",
                "kube-system",
                "--set",
                f"secrets-store-csi-driver.syncSecret.enabled=true",
                "--set",
                f"secrets-store-csi-driver.syncSecret.interval=2m"
            ],
            check=True,
            stderr=subprocess.PIPE,
            stdout=subprocess.PIPE,
            text=True
        )
        print("CSI Provider and CSI driver installed successfully." + result.stdout)
    except subprocess.CalledProcessError as e:
        print(f"Failed to install csi driver&provider: {e}")
        raise RuntimeError("Failed to install CSI:" + str(e.stderr)) from e
    
    #TODO: would be nice to check that everything was installed correctly here and have a loop to
    # retry until we can get the logs with the correct information
    time.sleep(5)
    try:
        result = subprocess.run(
            [
                "helm",
                "list",
                "-n",
                "kube-system"
            ],
            check=True,
            stderr=subprocess.PIPE,
            stdout=subprocess.PIPE,
            text=True
        )
        print("Helm list kube-system namespace successfully: \n" + result.stdout)
    except subprocess.CalledProcessError as e:
        raise RuntimeError("Failed to list helm charts installed in the kube-system namespace:" + str(e.stderr)) from e

def remove_dev_csi_secret_store_driver(csi_driver_name: str = "csi") -> None:
    try:
        # Delete pods forcefully
        subprocess.run(["kubectl", "delete", "pods", "--all", "--force"], check=True)

        # Delete CRDs
        subprocess.run(["kubectl", "delete", "crds",
                        "secretprovidercaches.secrets-store.csi.x-k8s.io",
                        "secretproviderclasses.secrets-store.csi.x-k8s.io",
                        "secretproviderclasspodstatuses.secrets-store.csi.x-k8s.io"], check=True)

        # Uninstall CSI driver using Helm
        subprocess.run(["helm", "uninstall", csi_driver_name, "-n", "kube-system"], check=True)

        print("CSI Secret Store driver and associated data successfully removed.")
    except subprocess.CalledProcessError as e:
        print(f"Error occurred while removing CSI Secret Store driver: {str(e)}")

def remove_none_images() -> int:
    try:
        # Run docker images command
        result = subprocess.run(["docker", "images"], capture_output=True, text=True, check=True)

        # Parse the output and extract image IDs tagged as <none>
        lines = result.stdout.strip().split("\n")
        image_ids = []
        for line in lines[1:]:
            parts = line.split()
            if len(parts) >= 2 and parts[1] == "<none>":
                image_ids.append(parts[2])

        # Remove the images tagged as <none>
        removed_count = 0
        for image_id in image_ids:
            try:
                subprocess.run(["docker", "rmi", image_id], check=True, capture_output=True, text=True)
                removed_count += 1
            except subprocess.CalledProcessError as e:
                error_message = str(e.stderr).lower()
                if "image is being used by" in error_message:
                    print(f"Skipped removal of image {image_id}: {str(e.stderr)}")
                else:
                    raise
        
        print(f"Removed {removed_count} image(s) tagged as <none>.")
    except subprocess.CalledProcessError as e:
        print("An error occurred while removing images:")
        print(str(e))
        raise

def main(run_full_flow: bool = False, only_run_export: bool = False, only_create_cluster: bool = False):
    subscription_id = args.subscription_id
    resource_group = args.resource_group
    keyvault_name = args.keyvault_name
    secret_name = args.secret_name
    managed_identity_name = args.managed_identity_name
    storage_account = args.storage_account
    storage_container = args.storage_container
    service_account_key_file=args.service_account_key_file
    service_account_signing_key_file=args.service_account_signing_key_file
    cluster_name = args.cluster_name
    clean = args.clean
    tenant_id = args.tenant_id
    install_wi = args.install_workload_identity_webhook
    service_account_wi = args.service_account_workload_identity
    service_account_wi_namespace = args.service_account_workload_identity_namespace
    unset_env_vars = args.unset_shell_environment_variables
    install_csi_default_driver_provider = args.install_csi_default_driver_provider
    uninstall_csi_default_driver_provider = args.uninstall_csi_default_driver_provider
    remove_dev_csi = args.remove_dev_csi
    rni = args.remove_none_images

    # Define the service account issuer
    service_account_issuer = f'https://{storage_account}.blob.core.windows.net/{storage_container}/'

    if only_create_cluster or only_run_export:
        # Check if all required arguments are provided
        if (
            subscription_id is None
            or resource_group is None
            or keyvault_name is None
            or managed_identity_name is None
            or storage_account is None
            or storage_container is None
            or service_account_key_file is None
            or service_account_signing_key_file is None
            or service_account_issuer is None
            or cluster_name is None
            or tenant_id is None
        ):
            print("ERROR: All arguments must be provided.")
            sys.exit(1)
        service_account_key_file_full_path = str(Path(service_account_key_file).resolve())
        service_account_signing_key_file_full_path = str(Path(service_account_signing_key_file).resolve())

        export_environment_variables(
            resource_group=resource_group,
            keyvault_name=keyvault_name,
            secret_name=secret_name,
            managed_identity_name=managed_identity_name,
            storage_account=storage_account,
            storage_container=storage_container,
            service_account_issuer=service_account_issuer,
            subscription_id=subscription_id,
            service_account_key_file=service_account_key_file_full_path,
            service_account_signing_key_file=service_account_signing_key_file_full_path,
            cluster_name=cluster_name,
            tenant_id=tenant_id,
            export_to_shell=only_run_export
        )

    if run_full_flow:
        check_tool_installed('curl', 'openssl', 'azwi', 'az', 'kubectl', 'helm', 'kind')
        setup_kubernetes_with_keyvault(
            subscription_id=subscription_id,
            resource_group=resource_group,
            keyvault_name=keyvault_name,
            secret_name=secret_name,
            managed_identity_name=managed_identity_name,
            storage_account=storage_account,
            storage_container=storage_container,
            service_account_issuer=service_account_issuer,
            cluster_name=cluster_name,
            tenant_id=tenant_id
        )

        if run_full_flow or only_create_cluster:
            create_kind_cluster(cluster_name=cluster_name)
    if install_wi:
        if (service_account_wi is None or service_account_wi_namespace is None):
            print("ERROR: service_account_wi and service_account_wi_namespace must be provided.")
            sys.exit(1)
        
        set_workload_identity_env_vars(service_account_wi=service_account_wi, service_account_wi_namespace=service_account_wi_namespace)
        install_workload_identity_webhook()
        setup_webhook_workload_identity()
        test_webhook_integration()
        force_delete_pod("quick-start-wi-webhook-csi-secret")
    if install_csi_default_driver_provider:
        install_default_csi_driver_and_provider_bundle()
        create_secret_provider_class()
        create_pod_with_secret_provider_class()
    if uninstall_csi_default_driver_provider:
        force_delete_pod("test-csi-driver-installation-pod")
        uninstall_chart("default-csi-driver-and-provider-bundle")
    elif clean:
        delete_kind_cluster(cluster_name=cluster_name)
        unset_environment_variables()
    elif unset_env_vars:
        unset_environment_variables()
    elif remove_dev_csi:
        remove_dev_csi_secret_store_driver()
    elif rni:
        remove_none_images()
        


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Setup Kubernetes with Azure Key Vault integration using a user-assigned managed identity")
    parser.add_argument("-ti", "--tenant-id", type=str, help="Azure tenant ID")
    parser.add_argument("-s", "--subscription-id", type=str, default="MySubscription", help="Azure subscription ID [default: MySubscription]")
    parser.add_argument("-g","--resource-group", type=str, default="MyResourceGroup", help="Azure resource group name [default: MyResourceGroup]")
    parser.add_argument("-kv","--keyvault-name", type=str, default="MyKeyVault", help="Azure Key Vault name [default: MyKeyVault]")
    parser.add_argument("-sn","--secret-name", type=str, default="demo-csi-secret1", help="Secret name in Azure Key Vault [default: demo-csi-secret1] used to test that the webhook and csi installation were successful.")
    parser.add_argument("-mi","--managed-identity-name", type=str, default="MyManagedIdentity", help="User-assigned managed identity name [default: MyManagedIdentity]")
    parser.add_argument("-sa","--storage-account", type=str, default="MyStorageAccount", help="Azure Storage account name [default: MyStorageAccount]")
    parser.add_argument("-sc","--storage-container", type=str, default="MyStorageAccountContainer", help="Azure Storage account container name [default: MyStorageAccountContainer]")
    parser.add_argument("-k","--service-account-key-file", type=str, help="Path to the service account key file or just its name if running the full flow.")
    parser.add_argument("-sk","--service-account-signing-key-file", type=str, help="Path to the service account signing key file or just its name if running the full flow.")
    parser.add_argument("-rff","--run-full-flow", action="store_true", help="Run the full flow. If not set, all arguments must be provided.")
    parser.add_argument("-re","--run-export", action="store_true", help="Only export environment variables. All arguments must be provided. To export to shell run: eval `python3 pyconfig_script.py [all mandatory args] -re`")
    parser.add_argument("-cn","--cluster-name", type=str, default="kind", help="Name of the Kubernetes cluster [default: kind]")
    parser.add_argument("-clean","--clean", action="store_true", help="Clean up the Kubernetes cluster")
    parser.add_argument("-usev","--unset-shell-environment-variables", action="store_true", help="Unset shell environment variables. To unser run: eval `python3 pyconfig_script.py -usev`")
    parser.add_argument("-oc","--only-create-cluster", action="store_true", help="Only create the Kubernetes cluster, but make sure the environment variables are provided when running this option.")
    parser.add_argument("-v","--verbose", action="store_true", help="Verbose output")
    parser.add_argument("-icsiddp", "--install-csi-default-driver-provider", action="store_true", help="Install the Secret Store CSI default driver and the Secret Store CSI default provider: taken from the main branch of the Secret Store CSI official repo.")
    parser.add_argument("-rni", "--remove-none-images", action="store_true", help="Remove none images from the local docker registry.")
    parser.add_argument("-rdcsi", "--remove-dev-csi", action="store_true", help="Remove the dev CSI driver and associated data.")
    parser.add_argument("-iwiw","--install-workload-identity-webhook", action="store_true", help="Install the workload identity webhook components")
    parser.add_argument("-ucsiddp", "--uninstall-csi-default-driver-provider", action="store_true", help="Uninstall the Secret Store CSI driver and the Secret Store CSI provider.")
    parser.add_argument("-sawi","--service-account-workload-identity", type=str, default="workload-identity-sa-csi", help="The service account for the workload identity webhook")
    parser.add_argument("-sawin","--service-account-workload-identity-namespace", type=str, default="default", help="The service account namespace for the workload identity webhook")
    
    args = parser.parse_args()

    main(run_full_flow=args.run_full_flow, only_run_export=args.run_export)  # pylint: disable=no-value-for-parameter
    
