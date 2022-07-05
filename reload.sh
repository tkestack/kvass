#!/bin/bash

>$1-not-Ready
cat $1 | while read line
do
        echo $line
        promctl login $line
        p=`kubectl -n $line  get po  | grep -v Running | grep -v NAME`
        echo $p
        if [ "$p" != "" ] 
        then
                echo $line >> $1-not-Ready
        fi
done