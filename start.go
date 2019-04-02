package main

import (
	"errors"
	"flag"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/efs"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/resource"
	api_v1 "k8s.io/client-go/pkg/api/v1"
	meta_v1 "k8s.io/client-go/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

var (
	path      string
	region    string
	name      string
	namespace string
)

func main() {
	flag.StringVar(&path, "path", os.Getenv("NFS_PATH"), "NFS Mount path")
	flag.StringVar(&region, "region", os.Getenv("AWS_REGION"), "AWS region")
	flag.StringVar(&name, "name", os.Getenv("NFS_NAME"), "NFS Mount name")
	flag.StringVar(&namespace, "ns", os.Getenv("NFS_NAMESPACE"), "NFS namespace")
	flag.Parse()
	if region == "" {
		panic("region is empty")
	}
	if maybeExit() {
		return
	}
	efss := efs.New(session.New(), &aws.Config{Region: aws.String(region)})
	nfs, err := efss.DescribeFileSystems(&efs.DescribeFileSystemsInput{
		CreationToken: &name,
	})
	if err != nil {
		panic(err)
	}
	for _, i := range nfs.FileSystems {
		if *i.CreationToken == name {
			target(efss, *i.FileSystemId)
			return
		}
	}
	res, err := efss.CreateFileSystem(&efs.CreateFileSystemInput{
		CreationToken: &name,
	})
	if err != nil {
		panic(err)
	}
	_, err = efss.CreateTags(&efs.CreateTagsInput{
		FileSystemId: res.FileSystemId,
		Tags: []*efs.Tag{
			{
				Key:   aws.String("Name"),
				Value: &name,
			},
		},
	})
	if err != nil {
		panic(err)
	}
	target(efss, *res.FileSystemId)

}

func target(efss *efs.EFS, id string) {
	targets, err := efss.DescribeMountTargets(&efs.DescribeMountTargetsInput{
		FileSystemId: &id,
	})
	if err != nil {
		panic(err)
	}
	if len(targets.MountTargets) > 0 {
		mount(*targets.MountTargets[0].IpAddress)
		return
	}
	in := mountConfig()
	in.FileSystemId = &id
	target, err := efss.CreateMountTarget(in)
	if err != nil {
		panic(err)
	}
	mount(*target.IpAddress)

}
func maybeExit() bool {
	config, err := rest.InClusterConfig()
	if err != nil {
		panic("Can't get kubernetes client:" + err.Error())
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic("Can't get kubernetes client:" + err.Error())
	}
	_, err = clientset.PersistentVolumes().Get(name, meta_v1.GetOptions{})
	if err != nil {
		return false
	}
	_, err = clientset.PersistentVolumeClaims(namespace).Get(name, meta_v1.GetOptions{})
	if err != nil {
		return false
	}
	return true
}
func mount(ip string) {
	config, err := rest.InClusterConfig()
	if err != nil {
		panic("Can't get kubernetes client:" + err.Error())
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic("Can't get kubernetes client:" + err.Error())
	}
	labels := map[string]string{
		"nfs-name": name,
	}
	q, err := resource.ParseQuantity("10240Mi")
	if err != nil {
		panic("Can't parse quantati:" + err.Error())
	}
	if _, err := clientset.PersistentVolumes().Get(name, meta_v1.GetOptions{}); err != nil {
		_, err = clientset.PersistentVolumes().Create(&api_v1.PersistentVolume{
			ObjectMeta: api_v1.ObjectMeta{
				Name:      name,
				Labels:    labels,
				Namespace: namespace,
			},
			Spec: api_v1.PersistentVolumeSpec{
				AccessModes: []api_v1.PersistentVolumeAccessMode{api_v1.ReadWriteMany},
				Capacity: map[api_v1.ResourceName]resource.Quantity{
					api_v1.ResourceName("storage"): q,
				},
				PersistentVolumeSource: api_v1.PersistentVolumeSource{
					NFS: &api_v1.NFSVolumeSource{
						Path:     path,
						ReadOnly: false,
						Server:   ip,
					},
				},
			},
		})
		if err != nil {
			panic("Can't create persistance volume:" + err.Error())
		}
	}
	/*if _, err := clientset.PersistentVolumeClaims(namespace).Get(name, meta_v1.GetOptions{}); err != nil {
		_, err = clientset.PersistentVolumeClaims(namespace).Create(&api_v1.PersistentVolumeClaim{
			ObjectMeta: api_v1.ObjectMeta{
				Name:        name,
				Labels:      labels,
				Namespace:   namespace,
				Annotations: map[string]string{"volume.alpha.kubernetes.io/storage-class": "anything"},
			},
			Spec: api_v1.PersistentVolumeClaimSpec{
				AccessModes: []api_v1.PersistentVolumeAccessMode{api_v1.ReadWriteMany},

				Resources: api_v1.ResourceRequirements{
					Requests: map[api_v1.ResourceName]resource.Quantity{
						api_v1.ResourceName("storage"): q,
					},
				},
				Selector: &meta_v1.LabelSelector{
					MatchLabels: labels,
				},
			},
		})
		if err != nil {
			panic("Can't create persistance volume claim:" + err.Error())
		}
	}*/
}

func mountConfig() *efs.CreateMountTargetInput {
	res, err := http.Get("http://169.254.169.254/latest/meta-data/instance-id")
	if err != nil {
		panic(err)
	}
	if res.StatusCode != http.StatusOK {
		panic(errors.New("Can't get instanceid: " + res.Status))
	}
	defer res.Body.Close()
	bid, err := ioutil.ReadAll(res.Body)
	if err != nil {
		panic(err)
	}
	//http://169.254.169.254/latest/meta-data/instance-id
	ec2s := ec2.New(session.New(), &aws.Config{Region: aws.String(region)})
	out, err := ec2s.DescribeInstances(&ec2.DescribeInstancesInput{
		InstanceIds: []*string{aws.String(string(bid))},
	})
	if err != nil {
		panic(err)
	}
	if len(out.Reservations) > 0 && len(out.Reservations[0].Instances) > 0 {
		i := out.Reservations[0].Instances[0]
		groups := []*string{}
		for _, s := range i.SecurityGroups {
			groups = append(groups, s.GroupId)
		}
		return &efs.CreateMountTargetInput{
			SubnetId:       i.SubnetId,
			SecurityGroups: groups,
		}

	}
	panic("Can't get instace id=" + string(bid))
}
