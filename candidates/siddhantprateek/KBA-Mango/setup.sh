if command -v npm >/dev/null 2>&1; then
    npm install -g @hyperledger-labs/weft
    if command -v nvm >/dev/null 2>&1; then
        nvm use 18
        npm install -g @hyperledger-labs/weft
    else
        echo "nvm is not installed. Skipping version switch."
    fi
else
    echo "npm is not installed. Please install npm and try again."
fi

curl -s http://console.127-0-0-1.nip.io:8080/ak/api/v1/components | weft microfab -w ./_wallets -p ./_gateways -m ./_msp -f

export CORE_PEER_LOCALMSPID=ProducersOrgMSP
export CORE_PEER_MSPCONFIGPATH=${PWD}/_msp/ProducersOrg/producersorgadmin/msp
export CORE_PEER_ADDRESS=producersorgpeer-api.127-0-0-1.nip.io:8080
export CORE_PEER_MSPCONFIGPATH=/Users/siddhantprateek/Documents/projects/Hyperledger-BiniBFT/candidates/siddhantprateek/KBA-Mango/_msp/ProducersOrg/producersorgadmin/msp

curl -sSL https://raw.githubusercontent.com/hyperledger/fabric/main/scripts/install-fabric.sh | bash -s -- binary

export PATH=$PATH:${PWD}/bin
export FABRIC_CFG_PATH=${PWD}/config