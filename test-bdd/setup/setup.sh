#!/usr/bin/env bash
set -e
set -o pipefail

function usage()
{
   cat << EOF
   Usage: $0 [--region AWS_REGION] [--vpc-id VPC_ID] [--cluster-name EKS_CLUSTER_NAME]
             [--ami-id AMI_ID] [--cluster-subnets CLUSTER_SUBNETS] [--node-subnets NODE_SUBNETS]
             [--keypair-name KEYPAIR_NAME] [--template-path TEMPLATE_PATH] OPERATION

   optional arguments:
     -h, --help             show this help message and exit
     --region               AWS region to set
     --vpc-id               VPC ID to deploy in
     --cluster-name         EKS Cluster Name
     --ami-id               AMI for worker nodes
     --cluster-subnets      Comma separated list of control plane subnets
     --node-subnets         Comma separated list of worker node subnets
     --keypair-name         Name of keypair to create/use
     --template-path        Absolute path to templates folder under repo/docs/cloudformation
     operation              Positional: either "create" or "delete"
EOF
    exit 0
}

function validate()
{
    if [[ -z ${REGION} ]];
    then
        echo "ERROR: --region was not provided"
        usage
    fi
    if [[ -z ${VPC_ID} ]];
    then
        echo "ERROR: --vpc-id was not provided"
        usage
    fi
    if [[ -z ${EKS_CLUSTER_NAME} ]];
    then
        echo "ERROR: --cluster-name was not provided"
        usage
    fi
    if [[ -z ${AMI_ID} ]];
    then
        echo "ERROR: --ami-id was not provided"
        usage
    fi
    if [[ -z ${CLUSTER_SUBNETS} ]];
    then
        echo "ERROR: --cluster-subnets was not provided"
        usage
    fi
    if [[ -z ${NODE_SUBNETS} ]];
    then
        echo "ERROR: --node-subnets was not provided"
        usage
    fi
    if [[ -z ${KEYPAIR_NAME} ]];
    then
        echo "ERROR: --keypair-name was not provided"
        usage
    fi
    if [[ -z ${TEMPLATE_PATH} ]];
    then
        echo "ERROR: --template-path was not provided"
        usage
    fi
    if [ ! -d "$TEMPLATE_PATH" ];
    then
        echo "ERROR: Could not find template path $TEMPLATE_PATH"
        exit 1
    fi
    if [[ -z ${POSITIONAL} ]];
    then
        echo "ERROR: operation was not provided"
        usage
    fi
    if [[ "$POSITIONAL" != "create" && "$POSITIONAL" != "delete" ]];
    then
        echo "ERROR: wrong operation ${POSITIONAL} was provided"
        usage
    fi
}

function parse_arguments()
{
    POSITIONAL=()
    while [[ $# -gt 0 ]]
    do
    key="$1"

    case $key in
        -h|--help)
        usage
        exit 0
        ;;
        --region)
        REGION="$2"
        shift
        shift
        ;;
        --vpc-id)
        VPC_ID="$2"
        shift
        shift
        ;;
        --cluster-name)
        EKS_CLUSTER_NAME="$2"
        shift
        shift
        ;;
        --ami-id)
        AMI_ID="$2"
        shift
        shift
        ;;
        --keypair-name)
        KEYPAIR_NAME="$2"
        shift
        shift
        ;;
        --template-path)
        TEMPLATE_PATH="$2"
        shift
        shift
        ;;
        --cluster-subnets)
        CLUSTER_SUBNETS="$2"
        CLUSTER_SUBNETS_ESCAPED=$(echo $2 | sed 's/,/\\,/g')
        CLUSTER_SUBNETS_LIST=$(echo $2 | jq -R -s -c 'split(",")' | sed 's/\\n//g')
        shift
        shift
        ;;
        --node-subnets)
        NODE_SUBNETS="$2"
        NODE_SUBNETS_ESCAPED=$(echo $2 | sed 's/,/\\,/g')
        NODE_SUBNETS_LIST=$(echo $2 | jq -R -s -c 'split(",")' | sed 's/\\n//g')
        shift
        shift
        ;;
        *)
        POSITIONAL+=("$1")
        shift
        ;;
    esac
    done
    set -- "${POSITIONAL[@]}" # restore positional parameters
}

function create()
{
    setup_controlplane_prereqs
    create_controlplane

    setup_nodegroup_prereqs
    create_nodegroup

    deploy_instance_manager
    deploy_argo
}

function delete()
{
    delete_instance_groups
    delete_compute
    delete_prereqs
    clean
}

function delete_instance_groups()
{
    echo "deleting instance groups..."
    kubectl delete --all instancegroups --namespace=instance-manager
}

function delete_compute()
{
    echo "deleting node groups and control plane..."
    if aws cloudformation describe-stacks --stack-name ${NODE_GROUP_STACK_NAME} > /dev/null 2>&1 ; then
        aws cloudformation delete-stack --stack-name ${NODE_GROUP_STACK_NAME}
    else
        echo "skipping ${NODE_GROUP_STACK_NAME} since it's already deleted"
    fi
    if aws cloudformation describe-stacks --stack-name ${EKS_CONTROL_PLANE_STACK_NAME} > /dev/null 2>&1 ; then
        aws cloudformation delete-stack --stack-name ${EKS_CONTROL_PLANE_STACK_NAME}
    else
        echo "skipping ${EKS_CONTROL_PLANE_STACK_NAME} since it's already deleted"
    fi
    aws cloudformation wait stack-delete-complete --stack-name ${NODE_GROUP_STACK_NAME}
    aws cloudformation wait stack-delete-complete --stack-name ${EKS_CONTROL_PLANE_STACK_NAME}
}

function delete_prereqs()
{
    echo "deleting service roles and security groups..."
    if aws cloudformation describe-stacks --stack-name ${EKS_SERVICE_ROLE_STACK_NAME} > /dev/null 2>&1 ; then
        aws cloudformation delete-stack --stack-name ${EKS_SERVICE_ROLE_STACK_NAME}
    else
        echo "skipping ${EKS_SERVICE_ROLE_STACK_NAME} since it's already deleted"
    fi
    if aws cloudformation describe-stacks --stack-name ${EKS_SECURITY_GROUP_STACK_NAME} > /dev/null 2>&1 ; then
        aws cloudformation delete-stack --stack-name ${EKS_SECURITY_GROUP_STACK_NAME}
    else
        echo "skipping ${EKS_SECURITY_GROUP_STACK_NAME} since it's already deleted"
    fi
    if aws cloudformation describe-stacks --stack-name ${NODE_GROUP_SERVICE_ROLE_STACK_NAME} > /dev/null 2>&1 ; then
        aws cloudformation delete-stack --stack-name ${NODE_GROUP_SERVICE_ROLE_STACK_NAME}
    else
        echo "skipping ${NODE_GROUP_SERVICE_ROLE_STACK_NAME} since it's already deleted"
    fi
    if aws cloudformation describe-stacks --stack-name ${NODE_SECURITY_GROUP_STACK_NAME} > /dev/null 2>&1 ; then
        aws cloudformation delete-stack --stack-name ${NODE_SECURITY_GROUP_STACK_NAME}
    else
        echo "skipping ${NODE_SECURITY_GROUP_STACK_NAME} since it's already deleted"
    fi
    aws cloudformation wait stack-delete-complete --stack-name ${EKS_SERVICE_ROLE_STACK_NAME}
    aws cloudformation wait stack-delete-complete --stack-name ${EKS_SECURITY_GROUP_STACK_NAME}
    aws cloudformation wait stack-delete-complete --stack-name ${NODE_GROUP_SERVICE_ROLE_STACK_NAME}
    aws cloudformation wait stack-delete-complete --stack-name ${NODE_SECURITY_GROUP_STACK_NAME}
}

function clean()
{
    echo "removing context..."
    CONTEXT=$(kubectl config current-context)
    kubectl config unset current-context
    kubectl config delete-cluster ${EKS_CLUSTER_NAME}
    kubectl config delete-context ${CONTEXT}
}

function deploy_instance_manager()
{
    echo "deploying instance-manager..."
    kubectl apply -f ../../docs/04_instance-manager.yaml
}

function deploy_argo()
{
    echo "deploying argo workflows..."
    kubectl create namespace argo
    kubectl apply -n argo -f https://raw.githubusercontent.com/argoproj/argo/stable/manifests/install.yaml
    kubectl create rolebinding default-admin --clusterrole=admin --serviceaccount=default:default
}

function setup_nodegroup_prereqs()
{

    if [ ! -f "$HOME/.ssh/$KEYPAIR_NAME.pem" ]; then
        # create an SSH keypair under ~/.ssh
        echo "creating keypair in $HOME/.ssh/$KEYPAIR_NAME.pem"
        aws ec2 create-key-pair --key-name ${KEYPAIR_NAME} --query 'KeyMaterial' \
        --output text > ~/.ssh/${KEYPAIR_NAME}.pem
    fi

    # node service role
    if ! aws cloudformation describe-stacks --stack-name ${NODE_GROUP_SERVICE_ROLE_STACK_NAME} > /dev/null 2>&1 ; then
        echo "creating node group service role..."
        aws cloudformation create-stack \
        --stack-name ${NODE_GROUP_SERVICE_ROLE_STACK_NAME} \
        --capabilities CAPABILITY_IAM \
        --template-body file://$TEMPLATE_PATH/02_node-group-service-role.yaml
    else
        echo "skipping ${NODE_GROUP_SERVICE_ROLE_STACK_NAME} since it already exists"
    fi
    aws cloudformation wait stack-create-complete --stack-name ${NODE_GROUP_SERVICE_ROLE_STACK_NAME}

    # get service role arn
    NODE_GROUP_ROLE_ARN=$(aws cloudformation describe-stacks \
    --stack-name ${NODE_GROUP_SERVICE_ROLE_STACK_NAME} \
    --query "Stacks[0].Outputs[?OutputKey=='NodeGroupRoleArn'].OutputValue" \
    --output text)

    # get service role name
    NODE_GROUP_ROLE_NAME=$(aws cloudformation describe-stacks \
    --stack-name ${NODE_GROUP_SERVICE_ROLE_STACK_NAME} \
    --query "Stacks[0].Outputs[?OutputKey=='NodeGroupRoleName'].OutputValue" \
    --output text)

    if ! aws cloudformation describe-stacks --stack-name ${NODE_SECURITY_GROUP_STACK_NAME} > /dev/null 2>&1 ; then
        echo "creating node group security group..."
        # node security group
        aws cloudformation create-stack \
        --stack-name ${NODE_SECURITY_GROUP_STACK_NAME} \
        --template-body file://$TEMPLATE_PATH/02_node-group-security-group.yaml \
        --parameters ParameterKey=ClusterName,ParameterValue=${EKS_CLUSTER_NAME} \
        ParameterKey=VpcId,ParameterValue=${VPC_ID} \
        ParameterKey=ClusterControlPlaneSecurityGroup,ParameterValue=${EKS_SECURITY_GROUP_ID}
    else
        echo "skipping ${NODE_SECURITY_GROUP_STACK_NAME} since it already exists"
    fi

    aws cloudformation wait stack-create-complete --stack-name ${NODE_SECURITY_GROUP_STACK_NAME}

    # get security group id
    NODE_SECURITY_GROUP=$(aws cloudformation describe-stacks \
    --stack-name ${NODE_SECURITY_GROUP_STACK_NAME} \
    --query "Stacks[0].Outputs[?OutputKey=='SecurityGroup'].OutputValue" \
    --output text)
}

function setup_controlplane_prereqs()
{
    if ! aws cloudformation describe-stacks --stack-name ${EKS_SERVICE_ROLE_STACK_NAME} > /dev/null 2>&1 ; then
        echo "creating control plane service role..."
        aws cloudformation create-stack \
        --stack-name ${EKS_SERVICE_ROLE_STACK_NAME} \
        --template-body file://$TEMPLATE_PATH/00_eks-cluster-service-role.yaml \
        --capabilities CAPABILITY_IAM
    else
        echo "skipping ${EKS_SERVICE_ROLE_STACK_NAME} since it already exists"
    fi

    aws cloudformation wait stack-create-complete --stack-name ${EKS_SERVICE_ROLE_STACK_NAME}

    EKS_ROLE_ARN=$(aws cloudformation describe-stacks \
    --stack-name ${EKS_SERVICE_ROLE_STACK_NAME} \
    --query "Stacks[0].Outputs[?OutputKey=='RoleArn'].OutputValue" \
    --output text)

    if ! aws cloudformation describe-stacks --stack-name ${EKS_SECURITY_GROUP_STACK_NAME} > /dev/null 2>&1 ; then
        echo "creating control plane security group..."
        aws cloudformation create-stack \
        --stack-name ${EKS_SECURITY_GROUP_STACK_NAME} \
        --template-body file://$TEMPLATE_PATH/00_eks-cluster-security-group.yaml \
        --parameters ParameterKey=VpcId,ParameterValue=${VPC_ID}
    else
        echo "skipping ${EKS_SECURITY_GROUP_STACK_NAME} since it already exists"
    fi

    aws cloudformation wait stack-create-complete --stack-name ${EKS_SECURITY_GROUP_STACK_NAME}

    EKS_SECURITY_GROUP_ID=$(aws cloudformation describe-stacks \
    --stack-name ${EKS_SECURITY_GROUP_STACK_NAME} \
    --query "Stacks[0].Outputs[?OutputKey=='SecurityGroup'].OutputValue" \
    --output text)
}

function create_controlplane()
{
    if ! aws cloudformation describe-stacks --stack-name ${EKS_CONTROL_PLANE_STACK_NAME} > /dev/null 2>&1 ; then
        echo "creating control plane $EKS_CLUSTER_NAME..."
        aws cloudformation create-stack \
        --stack-name ${EKS_CONTROL_PLANE_STACK_NAME} \
        --template-body file://$TEMPLATE_PATH/01_eks-cluster.yaml \
        --parameters ParameterKey=ClusterName,ParameterValue=${EKS_CLUSTER_NAME} \
        ParameterKey=ServiceRoleArn,ParameterValue=${EKS_ROLE_ARN} \
        ParameterKey=SecurityGroupIds,ParameterValue=${EKS_SECURITY_GROUP_ID} \
        ParameterKey=Subnets,ParameterValue=${CLUSTER_SUBNETS_ESCAPED} \
        ParameterKey=Version,ParameterValue=${VERSION}
    else
        echo "skipping ${EKS_CONTROL_PLANE_STACK_NAME} since it already exists"
    fi

    aws cloudformation wait stack-create-complete --stack-name ${EKS_CONTROL_PLANE_STACK_NAME}

    # update kubeconfig
    echo "setting context..."
    aws eks update-kubeconfig --name ${EKS_CLUSTER_NAME}
    kubectl config set-context $(kubectl config current-context) --namespace instance-manager
}

function create_nodegroup()
{
    export NODE_GROUP_NAME=MyBootstrapNodeGroup

    if ! aws cloudformation describe-stacks --stack-name ${NODE_GROUP_STACK_NAME} > /dev/null 2>&1 ; then
        echo "creating node group $NODE_GROUP_NAME..."
        aws cloudformation create-stack \
        --stack-name ${NODE_GROUP_STACK_NAME} \
        --template-body file://$TEMPLATE_PATH/03_node-group.yaml \
        --capabilities CAPABILITY_NAMED_IAM \
        --tags Key=instancegroups.orkaproj.io/ClusterName,Value=${EKS_CLUSTER_NAME} \
        --parameters ParameterKey=VpcId,ParameterValue=${VPC_ID} \
        ParameterKey=NodeGroupName,ParameterValue=${NODE_GROUP_NAME} \
        ParameterKey=ClusterControlPlaneSecurityGroup,ParameterValue=${EKS_SECURITY_GROUP_ID} \
        ParameterKey=NodeSecurityGroup,ParameterValue=${NODE_SECURITY_GROUP} \
        ParameterKey=Subnets,ParameterValue=${NODE_SUBNETS_ESCAPED} \
        ParameterKey=ClusterName,ParameterValue=${EKS_CLUSTER_NAME} \
        ParameterKey=KeyName,ParameterValue=${KEYPAIR_NAME} \
        ParameterKey=NodeImageId,ParameterValue=${AMI_ID} \
        ParameterKey=NodeInstanceRoleName,ParameterValue=${NODE_GROUP_ROLE_NAME} \
        ParameterKey=NodeInstanceRoleArn,ParameterValue=${NODE_GROUP_ROLE_ARN}
    else
        echo "skipping ${NODE_GROUP_STACK_NAME} since it already exists"
    fi

    aws cloudformation wait stack-create-complete --stack-name ${NODE_GROUP_STACK_NAME}

    cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: aws-auth
  namespace: kube-system
data:
  mapRoles: |
    - rolearn: $NODE_GROUP_ROLE_ARN
      username: system:node:{{EC2PrivateDNSName}}
      groups:
        - system:bootstrappers
        - system:nodes
EOF
}

function main()
{
    parse_arguments $@
    validate

    echo "OPERATION       = ${POSITIONAL}"
    echo "TEMPLATE PATH  = ${TEMPLATE_PATH}"
    echo "REGION          = ${REGION}"
    echo "CLUSTER NAME    = ${EKS_CLUSTER_NAME}"
    echo "VPC ID          = ${VPC_ID}"
    echo "AMI ID          = ${AMI_ID}"
    echo "CLUSTER SUBNETS = ${CLUSTER_SUBNETS}"
    echo "NODE SUBNETS    = ${NODE_SUBNETS}"
    echo "KEYPAIR NAME    = ${KEYPAIR_NAME}"

    export AWS_REGION=$REGION

    VERSION=1.13
    NODE_GROUP_STACK_NAME="eks-bootstrap-node-group"
    EKS_CONTROL_PLANE_STACK_NAME="eks-control-plane"
    EKS_SERVICE_ROLE_STACK_NAME="eks-service-role"
    EKS_SECURITY_GROUP_STACK_NAME="eks-security-group"
    NODE_GROUP_SERVICE_ROLE_STACK_NAME="eks-node-service-role"
    NODE_SECURITY_GROUP_STACK_NAME="eks-node-security-group"

    if [[ "$POSITIONAL" == "create" ]];
    then
        echo "starting create"
        create
    fi

    if [[ "$POSITIONAL" == "delete" ]];
    then
        echo "starting delete"
        delete
    fi

}

main $@