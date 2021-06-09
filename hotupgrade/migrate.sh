#! /bin/sh

if [ "${SIDECARSET_VERSION}" == "0" ]; then
  echo "empty image"
  exit 0
fi

for a in `seq 60`;
do
 if [ -e "./result" ];then
   echo "check start success"
   rm -f ./result
   exit 0
 fi
 sleep 1
done

echo "start failed"
exit 1
