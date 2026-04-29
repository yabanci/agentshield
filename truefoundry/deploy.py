"""
Deploy AgentShield to TrueFoundry.

Prerequisites:
    pip install truefoundry
    tfy login

Usage:
    python truefoundry/deploy.py --workspace <WORKSPACE_FQN>

The OLLAMA_URL secret must be created in your TrueFoundry workspace:
    tfy secret create --name OLLAMA_URL --value "http://<your-ollama-host>:11434"
"""

import argparse
import logging
from truefoundry.deploy import (
    Build,
    DockerFileBuild,
    HttpProbe,
    Port,
    Resources,
    Service,
    ServiceLivenessProbe,
    ServiceReadinessProbe,
    StringDataOrSecretRef,
    SecretRef,
)

logging.basicConfig(level=logging.INFO)

def deploy(workspace_fqn: str, image_tag: str = "latest") -> None:
    service = Service(
        name="agentshield",
        image=Build(
            build_spec=DockerFileBuild(
                dockerfile_path="Dockerfile",
                build_context_path=".",
            )
        ),
        ports=[
            Port(
                port=8080,
                expose=True,
                app_protocol="HTTP",
                host=None,  # TrueFoundry assigns a hostname
            )
        ],
        resources=Resources(
            cpu_request=0.25,
            cpu_limit=1.0,
            memory_request=256,
            memory_limit=512,
        ),
        env={
            "PORT": "8080",
            # Provide the URL of your Ollama instance.
            # For TrueFoundry-hosted Ollama, use the internal service URL.
            # For local Ollama: create a secret and reference it below.
            "OLLAMA_URL": StringDataOrSecretRef(
                value_from=SecretRef(secret_fqn="secret:OLLAMA_URL")
            ),
        },
        liveness_probe=ServiceLivenessProbe(
            config=HttpProbe(path="/health", port=8080),
            initial_delay_seconds=10,
            period_seconds=15,
            failure_threshold=3,
        ),
        readiness_probe=ServiceReadinessProbe(
            config=HttpProbe(path="/health", port=8080),
            initial_delay_seconds=5,
            period_seconds=10,
        ),
        replicas=1,
    )

    logging.info("Deploying AgentShield to workspace: %s", workspace_fqn)
    service.deploy(workspace_fqn=workspace_fqn)
    logging.info("Deployment triggered. Monitor at https://app.truefoundry.com")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Deploy AgentShield to TrueFoundry")
    parser.add_argument("--workspace", required=True, help="TrueFoundry workspace FQN")
    args = parser.parse_args()
    deploy(workspace_fqn=args.workspace)
