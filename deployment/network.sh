#!/bin/bash
# ==============================================================================
# Hyperledger Fabric BiniBFT Test Network Orchestrator
# ==============================================================================

set -e

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
export PATH=${DIR}/../bin:${DIR}:$PATH
export FABRIC_CFG_PATH=${DIR}

CHANNEL_NAME="binibft-channel"

function printHeader() {
    echo "======================================================================"
    echo " $1"
    echo "======================================================================"
}

function generateCerts() {
    printHeader "1. Generating Crypto Artifacts with cryptogen..."
    if [ -d "${DIR}/crypto-config" ]; then
        rm -rf "${DIR}/crypto-config"
    fi
    cryptogen generate --config="${DIR}/crypto-config.yaml" --output="${DIR}/crypto-config"
    echo "Crypto material successfully generated in ${DIR}/crypto-config"
}

function generateChannelArtifacts() {
    printHeader "2. Generating Genesis Block and Channel Artifacts..."
    mkdir -p "${DIR}/channel-artifacts"

    # Generate Genesis Block with BiniBFT consensus metadata
    echo "Generating Orderer Genesis Block (TwoOrgsBiniBFTOrdererGenesis)..."
    configtxgen -profile TwoOrgsBiniBFTOrdererGenesis -channelID system-channel -outputBlock "${DIR}/genesis.block"

    # Generate Channel Creation Transaction
    echo "Generating Channel Creation Transaction (${CHANNEL_NAME})..."
    configtxgen -profile TwoOrgsChannel -channelID "${CHANNEL_NAME}" -outputCreateChannelTx "${DIR}/channel-artifacts/${CHANNEL_NAME}.tx"

    # Generate Anchor Peer Transactions
    echo "Generating Anchor Peer Transaction for Org1MSP..."
    configtxgen -profile TwoOrgsChannel -channelID "${CHANNEL_NAME}" -outputAnchorPeersUpdate "${DIR}/channel-artifacts/Org1MSPanchors.tx" -asOrg Org1MSP

    echo "Generating Anchor Peer Transaction for Org2MSP..."
    configtxgen -profile TwoOrgsChannel -channelID "${CHANNEL_NAME}" -outputAnchorPeersUpdate "${DIR}/channel-artifacts/Org2MSPanchors.tx" -asOrg Org2MSP
}

function networkUp() {
    printHeader "3. Starting BiniBFT Fabric Cluster..."
    docker-compose -f "${DIR}/docker-compose-binibft.yaml" up -d
    docker ps -a --filter "network=binibft-net"
    echo "BiniBFT Fabric cluster is running."
}

function networkDown() {
    printHeader "Shutting Down Network and Cleaning Up..."
    docker-compose -f "${DIR}/docker-compose-binibft.yaml" down --volumes --remove-orphans
    rm -rf "${DIR}/crypto-config" "${DIR}/channel-artifacts" "${DIR}/genesis.block"
    echo "Clean up complete."
}

case "$1" in
    generate)
        generateCerts
        generateChannelArtifacts
        ;;
    up)
        generateCerts
        generateChannelArtifacts
        networkUp
        ;;
    down)
        networkDown
        ;;
    restart)
        networkDown
        generateCerts
        generateChannelArtifacts
        networkUp
        ;;
    *)
        echo "Usage: $0 {up|down|restart|generate}"
        exit 1
        ;;
esac
